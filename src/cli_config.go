package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	envDifyConfigDir = "DIFY_CONFIG_DIR"
	envXDGConfigHome = "XDG_CONFIG_HOME"
	loginFileName    = "login.yml"
)

// Where the shared folder came from, shown on the setup page.
type configSource string

const (
	sourceEnv     configSource = "env"
	sourceShell   configSource = "shell"
	sourceDefault configSource = "default"
	sourceManual  configSource = "manual"
)

// The user's own difyctl keeps its login in its config folder, wherever the binary
// is installed. QwenWork starts as a GUI app and does not see variables set in the
// user's shell, so the login shell is asked as well. Only absolute paths count.
func detectCLIConfigDir() (string, configSource) {
	if dir, err := validateConfigDir(os.Getenv(envDifyConfigDir)); err == nil {
		return dir, sourceEnv
	}
	shell := loginShellEnv(envDifyConfigDir, envXDGConfigHome)
	if dir, err := validateConfigDir(shell[envDifyConfigDir]); err == nil {
		return dir, sourceShell
	}
	dir, _ := validateConfigDir(defaultCLIConfigDir(shell[envXDGConfigHome]))
	return dir, sourceDefault
}

type savedLogin struct {
	Server string // empty when the saved server is not an HTTPS URL the connector accepts
	Email  string
}

func (l savedLogin) summary() string {
	if l.Email == "" {
		return ""
	}
	if l.Server == "" {
		return l.Email + " · 非 HTTPS 地址"
	}
	return l.Email + " · " + l.Server
}

// Reads only the top-level server and email of difyctl's login.yml; the token is
// never in this file.
func readSavedLogin(dir string) savedLogin {
	raw, err := os.ReadFile(filepath.Join(dir, loginFileName))
	if err != nil {
		return savedLogin{}
	}
	var login savedLogin
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimRight(line, "\r"), ":")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "server":
			login.Server, _ = validateServer(value)
		case "email":
			login.Email = value
		}
	}
	return login
}

// What the setup page shows for a shared folder: the detected one when none is
// given, or the one the user typed.
func sharedFolderInfo(requested string) (map[string]string, error) {
	dir, source := requested, sourceManual
	if requested == "" {
		dir, source = detectCLIConfigDir()
	} else {
		var err error
		if dir, err = validateConfigDir(requested); err != nil {
			return nil, err
		}
	}
	return map[string]string{
		"shared_config_dir": dir,
		"source":            string(source),
		"shared_login":      readSavedLogin(dir).summary(),
	}, nil
}
