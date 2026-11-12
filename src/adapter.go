// Package main adapts QwenWork desktop lifecycle commands to the official CLI.
// All business commands replace this process with difyctl without re-encoding IO.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const version = "0.2.1"

//go:embed portal.html
var portalHTML string

// These overrides are intentionally not exposed through production environment variables.
var vendorPath string
var profileRoot string

type adapterError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *adapterError) Error() string    { return e.Message }
func problem(code, message string) error { return &adapterError{code, message} }
func printJSON(value any)                { _ = json.NewEncoder(os.Stdout).Encode(value) }
func report(err error) int {
	var e *adapterError
	if !errors.As(err, &e) {
		e = &adapterError{"E-9999", "连接器异常；内部详情未输出。"}
	}
	_ = json.NewEncoder(os.Stderr).Encode(e)
	return 1
}
func root() (string, error) {
	path := profileRoot
	if path == "" {
		path = os.Getenv("QWEN_DIFY_CONFIG_DIR")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".config", "qwenwork-dify-connector")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return "", err
	}
	if err = os.Chmod(absolute, 0700); err != nil {
		return "", err
	}
	return absolute, nil
}
func binary() (string, error) {
	if vendorPath != "" {
		return vendorPath, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "vendor", "darwin_arm64", "difyctl")), nil
}
func cleanEnv() ([]string, error) {
	dir, err := root()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "DIFY_") {
			out = append(out, item)
		}
	}
	return append(out, "DIFY_CONFIG_DIR="+filepath.Join(dir, "difyctl")), nil
}
func marker() (string, error) {
	dir, err := root()
	return filepath.Join(dir, "authorization-required"), err
}
func needsAuthorization() (bool, error) {
	p, err := marker()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
func markRequired() error {
	p, err := marker()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}
func clearRequired() error {
	p, err := marker()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func validateServer(raw string) (string, error) {
	for _, r := range raw {
		if r <= 32 || r == 127 {
			return "", problem("E-0003", "请输入合法 HTTPS Dify 控制台地址。")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", problem("E-0003", "只接受不含账号、查询参数和片段的 HTTPS 控制台地址。")
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return "", problem("E-0003", "地址端口无效。")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
func saveServer(server string) error {
	dir, err := root()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "settings.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if err = json.NewEncoder(f).Encode(map[string]string{"server": server}); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "settings.json"))
}
func savedServer() string {
	dir, err := root()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return ""
	}
	var d struct {
		Server string `json:"server"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return ""
	}
	s, _ := validateServer(d.Server)
	return s
}

type upstreamResult struct {
	Data map[string]any
	Exit int
}

func invoke(ctx context.Context, args ...string) (upstreamResult, error) {
	path, err := binary()
	if err != nil {
		return upstreamResult{}, err
	}
	env, err := cleanEnv()
	if err != nil {
		return upstreamResult{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	command := exec.CommandContext(callCtx, path, append(args, "--json")...)
	command.Env = env
	// Authentication helpers must not leave descendants behind on cancellation.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		e := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	command.WaitDelay = 2 * time.Second
	command.Stdin = nil
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if callCtx.Err() != nil {
		return upstreamResult{}, problem("E-0004", "Dify 请求超时或已取消，请检查网络后重试。")
	}
	exit := 0
	if err != nil {
		var e *exec.ExitError
		if !errors.As(err, &e) {
			return upstreamResult{}, problem("E-0004", "无法启动官方 difyctl，请检查安装文件和执行权限。")
		}
		exit = e.ExitCode()
	}
	body := stdout.String()
	if strings.TrimSpace(body) == "" {
		body = stderr.String()
	}
	var data map[string]any
	if json.Unmarshal([]byte(body), &data) != nil || data == nil {
		return upstreamResult{}, problem("E-0004", "Dify CLI 未返回预期 JSON，详细输出未展示。")
	}
	return upstreamResult{data, exit}, nil
}
func object(value any) map[string]any { m, _ := value.(map[string]any); return m }
func str(value any) string            { s, _ := value.(string); return s }
func httpCode(value any) int          { f, _ := value.(float64); return int(f) }
func success(r upstreamResult) error {
	if r.Exit == 0 {
		return nil
	}
	e := object(r.Data["error"])
	code := str(e["code"])
	serverCode := str(object(e["server"])["code"])
	if httpCode(e["http_status"]) == 403 || code == "forbidden" || code == "permission_denied" || strings.HasSuffix(serverCode, "forbidden") {
		return problem("E-0005", "Dify 拒绝此操作，请检查当前用户和资源权限。")
	}
	if code == "not_logged_in" || httpCode(e["http_status"]) == 401 || r.Exit == 4 {
		return problem("E-0001", "Dify 未授权或会话失效，请在连接器页面认证。")
	}
	if r.Exit == 2 {
		return problem("E-0003", "命令参数有误，请先读取对应命令帮助。")
	}
	return problem("E-0004", "Dify 请求失败，原始错误未展示。")
}
func account(ctx context.Context, checkMarker bool) (map[string]string, error) {
	if checkMarker {
		required, err := needsAuthorization()
		if err != nil {
			return nil, err
		}
		if required {
			return nil, problem("E-0001", "连接向导未完成，请先完成当前 Dify 授权。")
		}
	}
	r, err := invoke(ctx, "describe", "account")
	if err != nil {
		return nil, err
	}
	if err = success(r); err != nil {
		return nil, err
	}
	data := r.Data
	if nested := object(data["data"]); nested != nil {
		data = nested
	}
	if nested := object(data["account"]); nested != nil {
		data = nested
	}
	out := map[string]string{}
	for _, k := range []string{"id", "name", "email"} {
		if value := str(data[k]); value != "" {
			out[k] = value
		}
	}
	if len(out) == 0 {
		return nil, problem("E-0004", "身份接口格式待适配，无法确认已连接。")
	}
	return out, nil
}
func authStatus(ctx context.Context) error {
	_, err := account(ctx, true)
	if err == nil {
		printJSON(map[string]string{"status": "authorized"})
		return nil
	}
	var e *adapterError
	if errors.As(err, &e) && (e.Code == "E-0001" || e.Code == "E-0002") {
		printJSON(map[string]string{"status": "unauthorized"})
		return nil
	}
	return err
}
func token() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

type portalState struct {
	sync.Mutex
	Stage           string            `json:"stage"`
	Message         string            `json:"message"`
	Server          string            `json:"server"`
	VerificationURI string            `json:"verification_uri,omitempty"`
	UserCode        string            `json:"user_code,omitempty"`
	Identity        map[string]string `json:"identity,omitempty"`
	ErrorCode       string            `json:"-"`
}

func (s *portalState) fail(err error) {
	var e *adapterError
	if !errors.As(err, &e) {
		e = &adapterError{"E-9999", "连接异常，内部详情未输出。"}
	}
	s.Lock()
	s.Stage = "error"
	s.Message = e.Message
	s.ErrorCode = e.Code
	s.Unlock()
}
func (s *portalState) snapshot() map[string]any {
	s.Lock()
	defer s.Unlock()
	return map[string]any{"stage": s.Stage, "message": s.Message, "server": s.Server, "verification_uri": s.VerificationURI, "user_code": s.UserCode, "identity": s.Identity}
}
func asInt(data any, fallback int) int {
	if f, ok := data.(float64); ok {
		return int(f)
	}
	return fallback
}
func runAuthorization(ctx context.Context, server string, state *portalState) {
	failed := func(e error) {
		if ctx.Err() == nil {
			state.fail(e)
		}
	}
	if err := saveServer(server); err != nil {
		failed(err)
		return
	}
	if err := markRequired(); err != nil {
		failed(err)
		return
	}
	r, err := invoke(ctx, "login", "--server", server, "--no-browser", "--no-wait", "--no-keyring")
	if err != nil {
		failed(err)
		return
	}
	if err = success(r); err != nil {
		failed(err)
		return
	}
	uri := str(r.Data["verification_uri_complete"])
	if uri == "" {
		uri = str(r.Data["verification_uri"])
	}
	code := str(r.Data["user_code"])
	u, parseErr := url.Parse(uri)
	base, _ := url.Parse(server)
	if parseErr != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, base.Host) || u.User != nil || code == "" {
		failed(problem("E-0004", "Dify 验证地址或设备码无效，或验证地址与控制台不同源。"))
		return
	}
	expires := asInt(r.Data["expires_in"], 600)
	if expires < 1 || expires > 900 {
		expires = 600
	}
	interval := asInt(r.Data["interval"], 5)
	if interval < 5 {
		interval = 5
	}
	if interval > 30 {
		interval = 30
	}
	state.Lock()
	state.Stage = "pending"
	state.Message = "请在 Dify 授权页面完成确认，本页会自动检测。"
	state.VerificationURI = uri
	state.UserCode = code
	state.Unlock()
	deadline := time.NewTimer(time.Duration(expires) * time.Second)
	defer deadline.Stop()
	for {
		wait := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case <-deadline.C:
			wait.Stop()
			failed(problem("E-0002", "授权码已过期，请重新点击认证。"))
			return
		case <-wait.C:
		}
		result, e := invoke(ctx, "login", "--resume")
		if e != nil {
			failed(e)
			return
		}
		if result.Exit == 0 && str(result.Data["status"]) == "pending" {
			continue
		}
		if e = success(result); e != nil {
			failed(e)
			return
		}
		identity, e := account(ctx, false)
		if e != nil {
			failed(e)
			return
		}
		if ctx.Err() != nil {
			return
		}
		if e = clearRequired(); e != nil {
			failed(e)
			return
		}
		state.Lock()
		state.Stage = "authorized"
		state.Message = "已连接，身份接口验证成功。"
		state.Identity = identity
		state.Unlock()
		return
	}
}
func authPortal(ctx context.Context, args []string) error {
	if os.Getenv("QWENWORK_SANDBOX_ID") != "" {
		return problem("E-0003", "本包仅支持 Mac 桌面；Web 请使用平台 OAuth 适配。")
	}
	fs := flag.NewFlagSet("auth init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	serverArg := fs.String("server", "", "Dify console URL")
	noOpen := fs.Bool("no-open", false, "Do not open browser (testing)")
	seconds := fs.Int("timeout-seconds", 900, "Authorization timeout")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *seconds < 5 || *seconds > 900 {
		return problem("E-0003", "认证参数无效。")
	}
	server := savedServer()
	if *serverArg != "" {
		var err error
		server, err = validateServer(*serverArg)
		if err != nil {
			return err
		}
	}
	dir, err := root()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "auth.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return problem("E-0003", "已有连接向导正在运行，请先完成或关闭该向导。")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return problem("E-0004", "无法创建本机连接向导，请检查回环端口权限。")
	}
	state := &portalState{Stage: "setup", Message: "填写 Dify 控制台地址后发起连接。", Server: server}
	nonce := token()
	endpoint := "/" + token()
	host := listener.Addr().String()
	origin := "http://" + host
	portalCtx, cancel := context.WithTimeout(ctx, time.Duration(*seconds)*time.Second)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	closeRequested := make(chan struct{}, 1)
	write := func(w http.ResponseWriter, status int, kind string, body []byte) {
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
	jsonReply := func(w http.ResponseWriter, status int, v any) {
		body, _ := json.Marshal(v)
		write(w, status, "application/json; charset=utf-8", body)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			jsonReply(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case endpoint:
				body := strings.ReplaceAll(strings.ReplaceAll(portalHTML, "__CSRF__", nonce), "__BASE__", endpoint)
				write(w, 200, "text/html; charset=utf-8", []byte(body))
			case endpoint + "/state":
				jsonReply(w, 200, state.snapshot())
			default:
				jsonReply(w, 404, map[string]string{"error": "not_found"})
			}
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Origin") != origin || r.Header.Get("X-CSRF") != nonce {
			jsonReply(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if r.URL.Path == endpoint+"/close" {
			jsonReply(w, 200, map[string]bool{"ok": true})
			select {
			case closeRequested <- struct{}{}:
			default:
			}
			return
		}
		if r.URL.Path != endpoint+"/start" {
			jsonReply(w, 404, map[string]string{"error": "not_found"})
			return
		}
		var body struct {
			Server string `json:"server"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if decoder.Decode(&body) != nil {
			jsonReply(w, 400, map[string]string{"error": "invalid_server"})
			return
		}
		target, e := validateServer(body.Server)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": "invalid_server"})
			return
		}
		state.Lock()
		if state.Stage != "setup" {
			state.Unlock()
			jsonReply(w, 409, map[string]string{"error": "already_started"})
			return
		}
		// Block old credentials before returning from the request that starts a
		// new authorization; waiting for the worker would leave a small gap.
		if e = markRequired(); e != nil {
			state.Unlock()
			jsonReply(w, 500, map[string]string{"error": "profile_unavailable"})
			return
		}
		state.Stage = "starting"
		state.Message = "正在向 Dify 请求设备授权…"
		state.Server = target
		state.Unlock()
		workers.Add(1)
		go func() { defer workers.Done(); runAuthorization(portalCtx, target, state) }()
		jsonReply(w, 200, map[string]bool{"ok": true})
	})
	httpd := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpd.Serve(listener) }()
	defer func() {
		cancel()
		shutdownCtx, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		_ = httpd.Shutdown(shutdownCtx)
	}()
	address := origin + endpoint
	if *noOpen {
		printJSON(map[string]string{"local_setup_url": address})
	} else if exec.Command("open", address).Run() != nil {
		return problem("E-0004", "系统浏览器无法打开，请检查默认浏览器设置。")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var finishedAt time.Time
	for {
		select {
		case <-portalCtx.Done():
			return problem("E-0002", "连接向导已取消或超时。")
		case <-closeRequested:
			return problem("E-0002", "连接向导已关闭。")
		case e := <-serveErr:
			if e != nil && !errors.Is(e, http.ErrServerClosed) {
				return problem("E-0004", "本机连接向导服务异常。")
			}
		case <-ticker.C:
			state.Lock()
			stage, message, errorCode := state.Stage, state.Message, state.ErrorCode
			state.Unlock()
			if stage == "authorized" || stage == "error" {
				if finishedAt.IsZero() {
					finishedAt = time.Now()
				}
				if time.Since(finishedAt) < 3*time.Second {
					continue
				}
				if stage == "authorized" {
					printJSON(map[string]string{"status": "authorized"})
					return nil
				}
				if errorCode == "" {
					errorCode = "E-0004"
				}
				return problem(errorCode, message)
			}
		}
	}
}
func mainRun(args []string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 3 && args[0] == "auth" && args[1] == "show" && args[2] == "--status" {
		return authStatus(ctx)
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "init" {
		return authPortal(ctx, args[2:])
	}
	if len(args) == 2 && args[0] == "auth" && args[1] == "logout" {
		dir, err := root()
		if err != nil {
			return err
		}
		lock, err := os.OpenFile(filepath.Join(dir, "auth.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer lock.Close()
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			return problem("E-0003", "认证向导仍在运行，请先关闭向导，再退出连接器。")
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		r, err := invoke(ctx, "logout")
		if err != nil {
			return err
		}
		if err = success(r); err != nil {
			return err
		}
		printJSON(map[string]string{"status": "unauthorized"})
		return nil
	}
	if len(args) == 2 && args[0] == "identity" && args[1] == "whoami" {
		data, err := account(ctx, true)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	}
	if len(args) > 0 && args[0] == "cli" {
		args = args[1:]
	}
	required, err := needsAuthorization()
	if err != nil {
		return err
	}
	if required {
		return problem("E-0001", "连接向导未完成，请先认证；不会使用之前账号执行此操作。")
	}
	path, err := binary()
	if err != nil {
		return err
	}
	env, err := cleanEnv()
	if err != nil {
		return err
	}
	if err = syscall.Exec(path, append([]string{path}, args...), env); err != nil {
		return problem("E-0004", "无法启动官方 difyctl，请检查安装包和执行权限。")
	}
	return nil
}
func main() {
	if err := mainRun(os.Args[1:]); err != nil {
		os.Exit(report(err))
	}
}
