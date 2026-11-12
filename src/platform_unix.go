//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
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
	tool := "xdg-open"
	if runtime.GOOS == "darwin" {
		tool = "open"
	}
	return exec.Command(tool, address).Run()
}

func executableName(name string) string { return name }
