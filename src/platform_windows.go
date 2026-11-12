//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var platformKernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFileEx = platformKernel32.NewProc("LockFileEx")
var unlockFileEx = platformKernel32.NewProc("UnlockFileEx")
var getSystemDirectory = platformKernel32.NewProc("GetSystemDirectoryW")

func defaultProfileRoot() (string, error) {
	local, err := os.UserCacheDir() // LOCALAPPDATA, not the roaming profile.
	if err != nil {
		return "", err
	}
	return filepath.Join(local, "qwenwork-dify-connector"), nil
}

func lockFile(file *os.File) error {
	var overlap syscall.Overlapped
	// The kernel releases this byte-range lock when the process/handle closes.
	if ok, _, err := lockFileEx.Call(file.Fd(), 0x3, 0, 1, 0, uintptr(unsafe.Pointer(&overlap))); ok == 0 {
		return err
	}
	return nil
}

func unlockFile(file *os.File) {
	var overlap syscall.Overlapped
	_, _, _ = unlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
}

func lockBusy(err error) bool {
	return errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}

func openBrowser(address string) error {
	// Resolve the Windows system DLL by absolute path, avoiding package/CWD DLL
	// search. ShellExecuteW receives a single URL argument without a shell.
	buffer := make([]uint16, 32768)
	length, _, err := getSystemDirectory.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 || length >= uintptr(len(buffer)) {
		return err
	}
	dll := syscall.NewLazyDLL(filepath.Join(syscall.UTF16ToString(buffer[:length]), "shell32.dll"))
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(address)
	if err != nil {
		return err
	}
	result, _, err := dll.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return err
	}
	return nil
}

func executableName(name string) string { return name + ".exe" }
