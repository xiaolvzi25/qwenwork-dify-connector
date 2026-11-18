//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

const (
	shellEnvMarker  = "__DIFY_QWEN_ENV__"
	shellEnvEnd     = shellEnvMarker + "END"
	shellEnvTimeout = 3 * time.Second
)

// Reads variables as the user's terminal sees them. Each value follows its own
// marker, so banners, values with newlines and logout scripts can't shift them.
// The end marker proves the values printed completely; then the output is used
// even if the shell later fails or is stopped by the timeout.
func loginShellEnv(names ...string) map[string]string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	script := ""
	for _, name := range names {
		script += "printf '\\n" + shellEnvMarker + name + "='; printf '%s' \"$" + name + "\"; "
	}
	script += "printf '\\n" + shellEnvEnd + "\\n'"
	ctx, cancel := context.WithTimeout(context.Background(), shellEnvTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, shell, "-ilc", script)
	command.WaitDelay = time.Second
	out, _ := command.Output()
	printed, _, complete := strings.Cut(string(out), "\n"+shellEnvEnd)
	if !complete {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	env := map[string]string{}
	for _, segment := range strings.Split(printed, "\n"+shellEnvMarker)[1:] {
		name, value, found := strings.Cut(segment, "=")
		if found && wanted[name] {
			env[name] = value
		}
	}
	return env
}

func defaultCLIConfigDir(shellXDG string) string {
	if runtime.GOOS == "linux" {
		xdg := shellXDG
		if xdg == "" {
			xdg = os.Getenv(envXDGConfigHome)
		}
		if xdg != "" {
			return filepath.Join(xdg, "difyctl")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "difyctl")
}
