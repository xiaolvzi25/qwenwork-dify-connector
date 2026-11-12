//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinuxBrowserDoesNotBlockPortalUntilBrowserExit(t *testing.T) {
	dir := t.TempDir()
	opener := filepath.Join(dir, "xdg-open")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\n/bin/sleep 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	started := time.Now()
	if err := openBrowser("http://127.0.0.1/setup"); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("browser opener blocked portal lifecycle")
	}
	// Let the deliberately persistent fake opener finish rather than orphan it.
	time.Sleep(2 * time.Second)
}

func TestLinuxBrowserReportsImmediateOpenerFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte("#!/bin/sh\nexit 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := openBrowser("http://127.0.0.1/setup"); err == nil {
		t.Fatal("failed desktop opener was reported as successful")
	}
}
