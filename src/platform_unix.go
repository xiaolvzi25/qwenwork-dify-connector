//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func defaultProfileRoot() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".config", "qwenwork-dify-connector"), err
}

func lockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func lockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func unlockFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func openBrowser(address string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", address).Run()
	}
	// Some Linux desktop openers stay attached for the browser's lifetime.
	// Reap the opener without blocking portal completion or cancellation.
	command := exec.Command("xdg-open", address)
	if err := command.Start(); err != nil {
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case err := <-finished:
		return err
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

func executableName(name string) string { return name }
