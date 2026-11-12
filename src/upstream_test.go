package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func assetForTest(body []byte, address string) upstreamAsset {
	sum := sha256.Sum256(body)
	return upstreamAsset{URL: address, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(body))}
}

type cachedBinaryFixture struct {
	path    string
	body    []byte
	receipt []byte
}

func previousCachedBinary(t *testing.T) cachedBinaryFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "difyctl")
	body := []byte("previous known-good cached binary\n")
	if err := os.WriteFile(path, body, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeReceipt(path, assetForTest(body, "")); err != nil {
		t.Fatal(err)
	}
	receipt, err := os.ReadFile(path + ".verified.json")
	if err != nil {
		t.Fatal(err)
	}
	return cachedBinaryFixture{path: path, body: body, receipt: receipt}
}

func assertCacheUnchanged(t *testing.T, fixture cachedBinaryFixture) {
	t.Helper()
	for path, want := range map[string][]byte{
		fixture.path:                    fixture.body,
		fixture.path + ".verified.json": fixture.receipt,
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("failed download changed %s", filepath.Base(path))
		}
	}
}

func assertNoPartialDownloads(t *testing.T, destination string) {
	t.Helper()
	partial, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".difyctl-download-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(partial) != 0 {
		t.Errorf("download left temporary files: %v", partial)
	}
}

func waitForDownload(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("local download did not finish")
		return nil
	}
}

func TestDownloadOfficialPublishesVerifiedBinaryAtomically(t *testing.T) {
	fixture := previousCachedBinary(t)
	body := []byte("replacement verified upstream binary\n")
	started := make(chan struct{})
	finish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("User-Agent") != "QwenWork-Dify-Connector/"+version {
			t.Error("download did not use the expected GET and connector User-Agent")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-finish:
			_, _ = w.Write(body[len(body)/2:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asset := assetForTest(body, server.URL)
	done := make(chan error, 1)
	go func() { done <- downloadOfficial(ctx, fixture.path, asset, server.Client()) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("local download did not start")
	}
	assertCacheUnchanged(t, fixture)
	close(finish)
	if err := waitForDownload(t, done); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("published binary differs: %v", err)
	}
	if err := verifyOfficial(fixture.path, asset); err != nil {
		t.Fatal(err)
	}
	if !receiptValid(fixture.path, asset) {
		t.Fatal("completed download did not produce a valid receipt")
	}
	info, err := os.Stat(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Errorf("binary permissions = %o, want 0700", info.Mode().Perm())
	}
	assertNoPartialDownloads(t, fixture.path)
}

func TestDownloadOfficialRejectsCorruptionWithoutReplacingCache(t *testing.T) {
	body := []byte("replacement verified upstream binary\n")
	for _, test := range []struct {
		name    string
		chunked bool
		mutate  func(*upstreamAsset)
	}{
		{name: "checksum", mutate: func(asset *upstreamAsset) { asset.SHA256 = strings.Repeat("0", 64) }},
		{name: "content-length", mutate: func(asset *upstreamAsset) { asset.Bytes++ }},
		{name: "chunked-short", chunked: true, mutate: func(asset *upstreamAsset) { asset.Bytes++ }},
		{name: "chunked-oversized", chunked: true, mutate: func(asset *upstreamAsset) { asset.Bytes-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := previousCachedBinary(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.chunked {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				}
				_, _ = w.Write(body)
			}))
			defer server.Close()
			asset := assetForTest(body, server.URL)
			test.mutate(&asset)
			if err := downloadOfficial(context.Background(), fixture.path, asset, server.Client()); err == nil {
				t.Fatal("corrupt download was accepted")
			}
			assertCacheUnchanged(t, fixture)
			assertNoPartialDownloads(t, fixture.path)
		})
	}
}

func TestDownloadOfficialInterruptedTransferPreservesCache(t *testing.T) {
	fixture := previousCachedBinary(t)
	body := []byte("replacement verified upstream binary\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = fmt.Fprintf(connection, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(body))
		_, _ = connection.Write(body[:len(body)/2])
	}))
	defer server.Close()
	if err := downloadOfficial(context.Background(), fixture.path, assetForTest(body, server.URL), server.Client()); err == nil {
		t.Fatal("interrupted transfer was accepted")
	}
	assertCacheUnchanged(t, fixture)
	assertNoPartialDownloads(t, fixture.path)
}

func TestDownloadOfficialCancellationPreservesCache(t *testing.T) {
	fixture := previousCachedBinary(t)
	body := []byte("replacement verified upstream binary\n")
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:1])
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- downloadOfficial(ctx, fixture.path, assetForTest(body, server.URL), server.Client())
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("local download did not start")
	}
	cancel()
	if err := waitForDownload(t, done); err == nil {
		t.Fatal("canceled download was accepted")
	}
	assertCacheUnchanged(t, fixture)
	assertNoPartialDownloads(t, fixture.path)
}

func TestReceiptInvalidatesChangedFileOrReceipt(t *testing.T) {
	for _, change := range []string{"checksum", "bytes", "timestamp", "malformed", "missing", "binary-timestamp", "binary-size", "binary-permission"} {
		t.Run(change, func(t *testing.T) {
			if change == "binary-permission" && runtime.GOOS == "windows" {
				t.Skip("Windows executable permissions are not Unix mode bits")
			}
			fixture := previousCachedBinary(t)
			asset := assetForTest(fixture.body, "")
			if !receiptValid(fixture.path, asset) {
				t.Fatal("fixture receipt is invalid")
			}
			var receipt binaryReceipt
			if err := json.Unmarshal(fixture.receipt, &receipt); err != nil {
				t.Fatal(err)
			}
			var err error
			switch change {
			case "checksum":
				receipt.SHA256 = strings.Repeat("0", 64)
			case "bytes":
				receipt.Bytes++
			case "timestamp":
				receipt.Modified++
			case "malformed":
				err = os.WriteFile(fixture.path+".verified.json", []byte("{"), 0600)
			case "missing":
				err = os.Remove(fixture.path + ".verified.json")
			case "binary-timestamp":
				changed := time.Unix(1900000000, 0)
				err = os.Chtimes(fixture.path, changed, changed)
			case "binary-size":
				err = os.WriteFile(fixture.path, append(fixture.body, '!'), 0700)
			case "binary-permission":
				err = os.Chmod(fixture.path, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "checksum" || change == "bytes" || change == "timestamp" {
				raw, err := json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture.path+".verified.json", raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if receiptValid(fixture.path, asset) {
				t.Fatal("changed file or receipt was trusted")
			}
		})
	}
}

func TestUpstreamCatalogPinsSupportedOfficialAssets(t *testing.T) {
	expected := map[string]struct {
		suffix string
		sha256 string
		bytes  int64
	}{
		"darwin_arm64":  {"darwin-arm64", "c9dd3e4ac2e5e5afa7e3ae6c2ab0794994c834438b25791fc8de13c614c13b85", 63151602},
		"darwin_amd64":  {"darwin-x64", "430374c00dcca3421b6793f4d04b23e6b9e54e76a618b19e2881d816cf75d6ee", 70267152},
		"linux_arm64":   {"linux-arm64", "ba696fb67bacce63aba9fa9ab9b67bbf4300429a5a5dfb13879938371dc22780", 84461864},
		"linux_amd64":   {"linux-x64", "2ab8eafb3c6870272fef0c9d1faea99181500905091a6c7b38e4e5c9822eb99b", 84792800},
		"windows_amd64": {"windows-x64.exe", "6f5cdea324737de4ab6e14a505a540e1795de1f722b8ecfeb329dc6398f0852f", 88377856},
	}
	var catalog upstreamCatalog
	if err := json.Unmarshal(upstreamCatalogJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Version != "1.18.0" || len(catalog.Platforms) != len(expected) {
		t.Fatalf("unexpected official version or platform count: %q, %d", catalog.Version, len(catalog.Platforms))
	}
	for target, pin := range expected {
		asset, ok := catalog.Platforms[target]
		if !ok {
			t.Errorf("catalog omits %s", target)
			continue
		}
		wantURL := "https://github.com/langgenius/dify/releases/download/1.18.0/difyctl-v1.18.0-" + pin.suffix
		if asset.URL != wantURL {
			t.Errorf("%s URL = %q, want pinned official URL", target, asset.URL)
		}
		digest, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(digest) != sha256.Size || asset.SHA256 != strings.ToLower(asset.SHA256) || asset.Bytes <= 0 {
			t.Errorf("%s lacks a valid SHA-256 or positive byte count", target)
		}
		if asset.SHA256 != pin.sha256 || asset.Bytes != pin.bytes {
			t.Errorf("%s differs from the verified official release pin", target)
		}
	}
}

func TestOfficialHTTPRedirectPolicy(t *testing.T) {
	client := officialHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("official download client has no redirect restrictions")
	}
	for _, test := range []struct {
		address string
		allowed bool
	}{
		{"https://github.com/langgenius/dify/releases/download/1.18.0/asset", true},
		{"https://release-assets.githubusercontent.com/download", true},
		{"https://objects.githubusercontent.com/download", true},
		{"https://github-releases.githubusercontent.com/download", true},
		{"https://GITHUB.COM/download", true},
		{"http://github.com/download", false},
		{"http://release-assets.githubusercontent.com/download", false},
		{"https://github.com.attacker.invalid/download", false},
		{"https://attacker.invalid/download", false},
		{"https://user:password@github.com/download", false},
	} {
		t.Run(test.address, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, test.address, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.CheckRedirect(request, nil)
			if (err == nil) != test.allowed {
				t.Errorf("redirect allowed = %v, want %v", err == nil, test.allowed)
			}
		})
	}
	request, _ := http.NewRequest(http.MethodGet, "https://github.com/download", nil)
	if err := client.CheckRedirect(request, make([]*http.Request, 8)); err == nil {
		t.Fatal("unbounded redirect chain was accepted")
	}
}
