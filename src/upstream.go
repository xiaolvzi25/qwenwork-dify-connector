package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed upstream-catalog.json
var upstreamCatalogJSON []byte

type upstreamAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type upstreamCatalog struct {
	Version   string                   `json:"version"`
	Platforms map[string]upstreamAsset `json:"platforms"`
}
type binaryReceipt struct {
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
	Modified int64  `json:"modified_ns"`
}

func currentUpstream() (upstreamCatalog, upstreamAsset, error) {
	var catalog upstreamCatalog
	if json.Unmarshal(upstreamCatalogJSON, &catalog) != nil {
		return catalog, upstreamAsset{}, problem("E-0004", "连接器官方 CLI 清单损坏，请重新安装。")
	}
	asset, ok := catalog.Platforms[runtime.GOOS+"_"+runtime.GOARCH]
	if !ok {
		return catalog, asset, problem("E-0003", "本安装包不支持当前系统或处理器架构。")
	}
	return catalog, asset, nil
}
func officialFilename() string {
	if runtime.GOOS == "windows" {
		return "difyctl.exe"
	}
	return "difyctl"
}
func cachedOfficialPath() (string, upstreamAsset, error) {
	catalog, asset, err := currentUpstream()
	if err != nil {
		return "", asset, err
	}
	dir, err := root()
	if err != nil {
		return "", asset, err
	}
	return filepath.Join(dir, "runtime", catalog.Version, runtime.GOOS+"_"+runtime.GOARCH, officialFilename()), asset, nil
}

// A new installation can report unauthorized immediately without a network call.
// Existing profiles still go through the official account check after upgrades.
func freshUninstalledProfile() (bool, error) {
	if vendorPath != "" {
		return false, nil
	}
	self, err := os.Executable()
	if err != nil {
		return false, err
	}
	bundled := filepath.Join(filepath.Dir(self), "..", "..", "vendor", runtime.GOOS+"_"+runtime.GOARCH, officialFilename())
	if _, err = os.Lstat(bundled); err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	cache, _, err := cachedOfficialPath()
	if err != nil {
		return false, err
	}
	if _, err = os.Lstat(cache); err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	dir, err := root()
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(filepath.Join(dir, "difyctl"))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

func receiptValid(path string, asset upstreamAsset) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Bytes {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0100 == 0 {
		return false
	}
	raw, err := os.ReadFile(path + ".verified.json")
	if err != nil {
		return false
	}
	var receipt binaryReceipt
	return json.Unmarshal(raw, &receipt) == nil && receipt.SHA256 == asset.SHA256 && receipt.Bytes == info.Size() && receipt.Modified == info.ModTime().UnixNano()
}
func writeReceipt(path string, asset upstreamAsset) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(binaryReceipt{asset.SHA256, info.Size(), info.ModTime().UnixNano()})
	if err != nil {
		return err
	}
	return os.WriteFile(path+".verified.json", raw, 0600)
}
func verifyOfficial(path string, asset upstreamAsset) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != asset.Bytes {
		return fmt.Errorf("invalid official CLI file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("official CLI checksum mismatch")
	}
	return nil
}

func downloadOfficial(ctx context.Context, destination string, asset upstreamAsset, client *http.Client) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "QwenWork-Dify-Connector/"+version)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("official download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > 0 && response.ContentLength != asset.Bytes {
		return fmt.Errorf("official download length mismatch")
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".difyctl-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, asset.Bytes+1))
	if err != nil {
		return err
	}
	if count != asset.Bytes || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("official download integrity check failed")
	}
	if err = file.Chmod(0700); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	// The exclusive download lock serializes replacements. Never execute a partial file.
	if err = os.Rename(file.Name(), destination); err != nil {
		return err
	}
	return writeReceipt(destination, asset)
}

func officialHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many download redirects")
			}
			host := strings.ToLower(request.URL.Hostname())
			allowed := host == "github.com" || host == "release-assets.githubusercontent.com" || host == "objects.githubusercontent.com" || host == "github-releases.githubusercontent.com"
			if request.URL.Scheme != "https" || !allowed || request.URL.User != nil {
				return fmt.Errorf("untrusted official download redirect")
			}
			return nil
		},
	}
}

func ensureOfficialCLI(ctx context.Context) (string, error) {
	path, asset, err := cachedOfficialPath()
	if err != nil {
		return "", err
	}
	if receiptValid(path, asset) {
		return path, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(path+".download.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	for {
		if err = lockFile(lock); err == nil {
			break
		}
		if !lockBusy(err) {
			return "", problem("E-0004", "无法锁定本机官方 CLI 缓存，请检查目录权限与文件系统。")
		}
		select {
		case <-ctx.Done():
			return "", problem("E-0002", "官方 CLI 安装已取消。")
		case <-time.After(150 * time.Millisecond):
		}
	}
	defer unlockFile(lock)
	// A hard host termination bypasses defers; clear only this installer's
	// abandoned temporary regular files while holding the exclusive cache lock.
	partials, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".difyctl-download-*"))
	for _, partial := range partials {
		if info, e := os.Lstat(partial); e == nil && info.Mode().IsRegular() {
			_ = os.Remove(partial)
		}
	}
	if receiptValid(path, asset) {
		return path, nil
	}
	if verifyOfficial(path, asset) == nil {
		if err = os.Chmod(path, 0700); err == nil {
			err = writeReceipt(path, asset)
		}
		if err == nil {
			return path, nil
		}
		return "", problem("E-0004", "无法准备本机官方 CLI 缓存，请检查文件权限。")
	}
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return "", problem("E-0004", "官方 CLI 缓存不是普通文件，请重新安装连接器。")
	}
	parsed, parseErr := url.Parse(asset.URL)
	if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() != "github.com" || parsed.User != nil {
		return "", problem("E-0004", "官方 CLI 下载清单无效。")
	}
	fmt.Fprintln(os.Stderr, "首次使用：正在下载并校验官方 difyctl 1.18.0；后续调用使用本机缓存。")
	if err = downloadOfficial(ctx, path, asset, officialHTTPClient()); err != nil {
		if ctx.Err() != nil {
			return "", problem("E-0002", "官方 CLI 安装已取消。")
		}
		return "", problem("E-0004", "官方 CLI 下载或校验失败。请检查 GitHub 发布地址的网络访问和本机磁盘空间后重试。")
	}
	return path, nil
}
