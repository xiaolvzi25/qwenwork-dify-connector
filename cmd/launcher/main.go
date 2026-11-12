// Command launcher exposes the package's stable top-level bin/dify-qwen entry.
// It replaces itself with the macOS Apple Silicon adapter without changing IO.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	self, err := os.Executable()
	if err == nil {
		target := filepath.Join(filepath.Dir(self), "darwin_arm64", "dify-qwen")
		err = syscall.Exec(target, append([]string{target}, os.Args[1:]...), os.Environ())
	}
	// Keep paths, environment variables and other local details out of errors.
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
		"code":    "E-0004",
		"message": "无法启动 Dify 连接器，请确认安装包完整并支持 macOS Apple Silicon。",
	})
	os.Exit(1)
}
