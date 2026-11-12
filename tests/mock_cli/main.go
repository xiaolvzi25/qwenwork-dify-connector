// Native test-only upstream. Never included in the runtime package.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

func emit(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
func has(args []string, needle string) bool {
	for _, arg := range args {
		if arg == needle {
			return true
		}
	}
	return false
}
func leaf() {
	// A console broadcast alone must not make the Job Object test pass. This
	// descendant deliberately ignores Ctrl+C/Break and needs tree cancellation.
	discardInterrupts()
	for count := 0; ; count++ {
		data, _ := json.Marshal(map[string]int{"pid": os.Getpid(), "tick": count})
		_ = os.WriteFile(os.Getenv("MOCK_HEARTBEAT"), data, 0600)
		time.Sleep(25 * time.Millisecond)
	}
}

func discardInterrupts() {
	// Notify keeps the Windows console handler installed for both Ctrl+C and
	// Ctrl+Break; deliberately consume events without terminating the mock.
	events := make(chan os.Signal, 8)
	signal.Notify(events, os.Interrupt)
	go func() {
		for range events {
		}
	}()
}
func spawnLeaf() *exec.Cmd {
	self, err := os.Executable()
	if err != nil {
		panic(err)
	}
	child := exec.Command(self)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "MOCK_MODE=") {
			child.Env = append(child.Env, item)
		}
	}
	child.Env = append(child.Env, "MOCK_MODE=leaf")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err = child.Start(); err != nil {
		panic(err)
	}
	return child
}
func main() {
	args := os.Args[1:]
	if len(args) == 3 && args[0] == "send-control" {
		pid, err := strconv.Atoi(args[1])
		if err != nil {
			panic(err)
		}
		event, err := strconv.Atoi(args[2])
		if err != nil {
			panic(err)
		}
		if err = sendControl(uint32(pid), uint32(event)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if path := os.Getenv("MOCK_LOG"); path != "" {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		_ = json.NewEncoder(file).Encode(args)
		_ = file.Close()
	}
	switch os.Getenv("MOCK_MODE") {
	case "", "inspect":
		env := map[string]string{}
		for _, item := range os.Environ() {
			if strings.HasPrefix(item, "DIFY_") {
				pair := strings.SplitN(item, "=", 2)
				env[pair[0]] = pair[1]
			}
		}
		emit(map[string]any{"args": args, "env": env})
	case "raw":
		_, _ = os.Stdout.Write([]byte("\x00raw\xff\n{\"event\":1}\n{\"event\":2}\n"))
		_, _ = os.Stderr.Write([]byte("error\xff\n"))
		os.Exit(23)
	case "stdin":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "stream":
		fmt.Println(`{"event":"first"}`)
		time.Sleep(2 * time.Second)
		fmt.Println(`{"event":"last"}`)
	case "signal":
		fmt.Println(os.Getpid())
		time.Sleep(30 * time.Second)
	case "account":
		fmt.Println(os.Getenv("MOCK_ACCOUNT"))
		code, _ := strconv.Atoi(os.Getenv("MOCK_EXIT"))
		os.Exit(code)
	case "portal":
		if has(args, "--no-wait") {
			uri := os.Getenv("MOCK_VERIFY_URI")
			if uri == "" {
				uri = "https://dify.example.invalid/activate"
			}
			emit(map[string]any{"verification_uri": uri, "user_code": "TEST-CODE", "expires_in": 30, "interval": 5})
		} else if has(args, "--resume") {
			emit(map[string]string{"status": "pending"})
		} else {
			emit(map[string]string{"id": "mock-user", "name": "Mock test"})
		}
	case "tree":
		discardInterrupts()
		child := spawnLeaf()
		emit(map[string]int{"parent_pid": os.Getpid(), "child_pid": child.Process.Pid})
		time.Sleep(30 * time.Second)
		_ = child.Process.Kill()
		_ = child.Wait()
	case "portal-tree":
		discardInterrupts()
		child := spawnLeaf()
		time.Sleep(30 * time.Second)
		_ = child.Process.Kill()
		_ = child.Wait()
	case "leaf":
		leaf()
	default:
		os.Exit(91)
	}
}
