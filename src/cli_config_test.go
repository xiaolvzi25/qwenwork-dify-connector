package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSavedLoginReadsOnlyTopLevelServerAndEmail(t *testing.T) {
	dir := t.TempDir()
	content := "server: 'https://dify.example.invalid'\r\nemail: \"user@example.invalid\"\naccount:\n  email: nested@example.invalid\ntoken_storage: file\n"
	if err := os.WriteFile(filepath.Join(dir, loginFileName), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	login := readSavedLogin(dir)
	if login.Server != "https://dify.example.invalid" || login.Email != "user@example.invalid" {
		t.Fatalf("login = %+v", login)
	}
	if got := login.summary(); got != "user@example.invalid · https://dify.example.invalid" {
		t.Errorf("summary = %q", got)
	}
}

func TestReadSavedLoginDropsAServerTheConnectorWouldNotAccept(t *testing.T) {
	for _, server := range []string{"http://dify.example.invalid", "not a url", ""} {
		dir := t.TempDir()
		content := "server: " + server + "\nemail: user@example.invalid\ninsecure: true\n"
		if err := os.WriteFile(filepath.Join(dir, loginFileName), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		login := readSavedLogin(dir)
		if login.Server != "" {
			t.Errorf("server %q kept as %q", server, login.Server)
		}
		if got := login.summary(); got != "user@example.invalid · 非 HTTPS 地址" {
			t.Errorf("summary = %q", got)
		}
	}
}

func TestReadSavedLoginWithoutAFileIsEmpty(t *testing.T) {
	if login := readSavedLogin(t.TempDir()); login != (savedLogin{}) || login.summary() != "" {
		t.Errorf("login = %+v", login)
	}
}

func TestValidateConfigDirRejectsRelativeAndControlCharacters(t *testing.T) {
	absolute, err := filepath.Abs("difyctl")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := validateConfigDir(absolute + string(filepath.Separator)); err != nil || got != absolute {
		t.Errorf("validateConfigDir(%q) = %q, %v", absolute, got, err)
	}
	for _, bad := range []string{"", "relative/difyctl", absolute + "\n"} {
		if _, err := validateConfigDir(bad); err == nil {
			t.Errorf("validateConfigDir(%q) accepted", bad)
		}
	}
}

func TestLoadSettingsDropsInvalidSavedValues(t *testing.T) {
	profileRoot = t.TempDir()
	t.Cleanup(func() { profileRoot = "" })
	raw := `{"server":"http://insecure.invalid","shared_config_dir":"relative/dir","future_field":true}`
	if err := os.WriteFile(filepath.Join(profileRoot, "settings.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(); got != (connectorSettings{}) {
		t.Errorf("settings = %+v, want empty", got)
	}
	dir, err := difyctlConfigDir()
	if err != nil || dir != filepath.Join(profileRoot, "difyctl") {
		t.Errorf("config dir = %q, %v; want the private profile", dir, err)
	}
}

func TestLoadSettingsKeepsTheValidValueWhenTheOtherIsBad(t *testing.T) {
	profileRoot = t.TempDir()
	t.Cleanup(func() { profileRoot = "" })
	raw := `{"server":"https://dify.example.invalid/","shared_config_dir":"relative/dir"}`
	if err := os.WriteFile(filepath.Join(profileRoot, "settings.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	want := connectorSettings{Server: "https://dify.example.invalid"}
	if got := loadSettings(); got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
}

func TestSharedFolderInfoRejectsATypedRelativePath(t *testing.T) {
	if _, err := sharedFolderInfo("relative/difyctl"); err == nil {
		t.Fatal("relative folder accepted")
	}
	dir := t.TempDir()
	info, err := sharedFolderInfo(dir)
	if err != nil || info["shared_config_dir"] != dir || info["source"] != string(sourceManual) || info["shared_login"] != "" {
		t.Errorf("info = %v, %v", info, err)
	}
}
