//go:build windows

package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
)

// Send only to a dedicated console created by the test harness, never the CI
// runner's console. References: Microsoft GenerateConsoleCtrlEvent/AttachConsole.
func sendControl(pid, event uint32) error {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	free := kernel.NewProc("FreeConsole")
	attach := kernel.NewProc("AttachConsole")
	setHandler := kernel.NewProc("SetConsoleCtrlHandler")
	generate := kernel.NewProc("GenerateConsoleCtrlEvent")
	_, _, _ = free.Call()
	ok, _, err := attach.Call(uintptr(pid))
	if ok == 0 {
		return fmt.Errorf("AttachConsole: %w", err)
	}
	defer free.Call()
	callback := syscall.NewCallback(func(kind uint32) uintptr { return 1 })
	ok, _, err = setHandler.Call(callback, 1)
	if ok == 0 {
		return fmt.Errorf("SetConsoleCtrlHandler: %w", err)
	}
	ok, _, err = generate.Call(uintptr(event), 0)
	if ok == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	time.Sleep(200 * time.Millisecond)
	runtime.KeepAlive(callback)
	return nil
}
