//go:build !windows

package main

import "errors"

func sendControl(pid, event uint32) error {
	return errors.New("Windows console control test requires Windows")
}
