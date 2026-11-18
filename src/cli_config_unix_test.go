//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A stand-in login shell: prints a banner, runs the script, then runs epilogue.
func fakeShell(t *testing.T, epilogue string) {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\necho 'Welcome banner __DIFY_QWEN_ENV__'\n/bin/sh -c \"$2\"\n" + epilogue + "\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
}

func TestLoginShellEnvSkipsBannersAndKeepsEmptyValues(t *testing.T) {
	fakeShell(t, "")
	t.Setenv(envDifyConfigDir, "/Users/someone/dify config")
	t.Setenv(envXDGConfigHome, "")
	env := loginShellEnv(envDifyConfigDir, envXDGConfigHome)
	if env[envDifyConfigDir] != "/Users/someone/dify config" {
		t.Errorf("DIFY_CONFIG_DIR = %q", env[envDifyConfigDir])
	}
	if value, ok := env[envXDGConfigHome]; !ok || value != "" {
		t.Errorf("XDG_CONFIG_HOME = %q, %v; want an empty value", value, ok)
	}
}

func TestLoginShellEnvKeepsANewlineInsideItsOwnValue(t *testing.T) {
	fakeShell(t, "")
	t.Setenv(envDifyConfigDir, "/first\n/second")
	t.Setenv(envXDGConfigHome, "/xdg")
	env := loginShellEnv(envDifyConfigDir, envXDGConfigHome)
	if env[envDifyConfigDir] != "/first\n/second" || env[envXDGConfigHome] != "/xdg" {
		t.Errorf("env = %q", env)
	}
	if _, err := validateConfigDir(env[envDifyConfigDir]); err == nil {
		t.Error("a folder with a newline must not be used")
	}
}

func TestLoginShellEnvUsesCompleteOutputEvenWhenTheShellFailsAfterwards(t *testing.T) {
	fakeShell(t, "echo 'logout script noise = x'; exit 3")
	t.Setenv(envDifyConfigDir, "/Users/someone/difyctl")
	if env := loginShellEnv(envDifyConfigDir); env[envDifyConfigDir] != "/Users/someone/difyctl" {
		t.Errorf("env = %q", env)
	}
}

func TestLoginShellEnvGivesNothingWhenTheShellFails(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	if env := loginShellEnv(envDifyConfigDir); len(env) != 0 {
		t.Errorf("env = %v, want none", env)
	}
}

func TestDetectCLIConfigDirSkipsARelativeValue(t *testing.T) {
	fakeShell(t, "")
	t.Setenv(envDifyConfigDir, "relative/difyctl")
	t.Setenv(envXDGConfigHome, "")
	t.Setenv("HOME", "/home/someone")
	dir, source := detectCLIConfigDir()
	if dir != "/home/someone/.config/difyctl" || source != sourceDefault {
		t.Errorf("detected %q from %s", dir, source)
	}
}

func TestDetectCLIConfigDirPrefersTheLoginShell(t *testing.T) {
	shellDir := t.TempDir()
	fakeShell(t, "")
	t.Setenv(envDifyConfigDir, "")
	// The fake shell inherits the test's environment, so set the value it will print
	// only for the child: an inherited empty value must not win.
	t.Setenv("ENV_FOR_SHELL", shellDir)
	shell := os.Getenv("SHELL")
	wrapper := filepath.Join(t.TempDir(), "wrapper")
	script := "#!/bin/sh\nDIFY_CONFIG_DIR=\"$ENV_FOR_SHELL\" exec " + shell + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", wrapper)
	dir, source := detectCLIConfigDir()
	if dir != shellDir || source != sourceShell {
		t.Errorf("detected %q from %s", dir, source)
	}
}
