// Command launcher exposes the package's stable top-level bin/dify-qwen entry.
// POSIX packages use a portable shell entry; this native entry serves Windows.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"qwenwork-dify-connector/internal/process"
)

func main() {
	self, err := os.Executable()
	if err == nil {
		name := "dify-qwen"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target := filepath.Join(filepath.Dir(self), runtime.GOOS+"_"+runtime.GOARCH, name)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		err = process.Replace(ctx, target, os.Args[1:], os.Environ())
	}
	// Keep paths, environment variables and other local details out of errors.
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
		"code":    "E-0004",
		"message": "无法启动 Dify 连接器，请确认安装包完整并支持当前桌面系统与架构。",
	})
	os.Exit(1)
}
