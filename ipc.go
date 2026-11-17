package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DaemonEndpoint stores IPC connection coordinates and authentication token.
type DaemonEndpoint struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func endpointMetaPath(dataDir string) string {
	return filepath.Join(dataDir, "daemon.json")
}

func generateAuthToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeEndpointAtomic(dataDir string, ep DaemonEndpoint) error {
	b, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(dataDir, fmt.Sprintf(".daemon.%d.tmp", os.Getpid()))
	if err := os.WriteFile(tmpPath, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, endpointMetaPath(dataDir))
}

func readEndpoint(dataDir string) (DaemonEndpoint, error) {
	var ep DaemonEndpoint
	b, err := os.ReadFile(endpointMetaPath(dataDir))
	if err != nil {
		return ep, err
	}
	if err := json.Unmarshal(b, &ep); err != nil {
		return ep, err
	}
	if ep.Network == "" || ep.Address == "" || ep.Token == "" {
		return ep, fmt.Errorf("incomplete daemon endpoint metadata")
	}
	return ep, nil
}

func listenDaemonIPC(dataDir string) (net.Listener, DaemonEndpoint, func(), error) {
	token, err := generateAuthToken()
	if err != nil {
		return nil, DaemonEndpoint{}, nil, fmt.Errorf("generate daemon auth token: %w", err)
	}

	sockPath := filepath.Join(dataDir, "daemon.sock")
	var listener net.Listener
	var network, addr string

	if preferUnixSocket(sockPath) {
		_ = os.Remove(sockPath)
		if l, err := net.Listen("unix", sockPath); err == nil {
			_ = os.Chmod(sockPath, 0o600)
			listener = l
			network = "unix"
			addr = sockPath
		}
	}

	if listener == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, DaemonEndpoint{}, nil, fmt.Errorf("listen loopback tcp: %w", err)
		}
		listener = l
		network = "tcp"
		addr = l.Addr().String()
	}

	ep := DaemonEndpoint{
		Network: network,
		Address: addr,
		Token:   token,
		PID:     os.Getpid(),
		Version: Version,
	}
	if err := writeEndpointAtomic(dataDir, ep); err != nil {
		_ = listener.Close()
		if network == "unix" {
			_ = os.Remove(sockPath)
		}
		return nil, DaemonEndpoint{}, nil, fmt.Errorf("write daemon metadata: %w", err)
	}

	cleanup := func() {
		_ = listener.Close()
		if current, err := readEndpoint(dataDir); err == nil && current.PID == ep.PID {
			_ = os.Remove(endpointMetaPath(dataDir))
		}
		if network == "unix" {
			_ = os.Remove(sockPath)
		}
	}

	return listener, ep, cleanup, nil
}

func authMiddleware(expectedToken string, next http.Handler) http.Handler {
	expectedHeader := []byte("Bearer " + expectedToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if len(got) != len(expectedHeader) || subtle.ConstantTimeCompare(got, expectedHeader) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized daemon request")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type daemonClient struct {
	cfg Config
}

func newDaemonClient(cfg Config) *daemonClient {
	return &daemonClient{cfg: cfg}
}

func (c *daemonClient) probeEndpoint() (DaemonEndpoint, string, bool) {
	ep, err := readEndpoint(c.cfg.DataDir)
	if err != nil {
		return DaemonEndpoint{}, "", false
	}
	var health struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	if err := c.doWithEndpoint(ctx, ep, http.MethodGet, "/health", nil, &health); err != nil || !health.OK {
		return DaemonEndpoint{}, "", false
	}
	return ep, health.Version, true
}

func (c *daemonClient) isRunning() bool {
	_, _, ok := c.probeEndpoint()
	return ok
}

func (c *daemonClient) ensureRunning() (DaemonEndpoint, error) {
	if ep, ver, ok := c.probeEndpoint(); ok {
		if ver == Version || ver == "" {
			return ep, nil
		}
		// Gracefully stop outdated daemon when binary was upgraded.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = c.doWithEndpoint(ctx, ep, http.MethodPost, "/stop", nil, nil)
		cancel()
		for range 20 {
			if _, _, stillAlive := c.probeEndpoint(); !stillAlive {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if err := os.MkdirAll(c.cfg.DataDir, 0o700); err != nil {
		return DaemonEndpoint{}, fmt.Errorf("create data dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(c.cfg.DataDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return DaemonEndpoint{}, fmt.Errorf("open daemon log: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		_ = logFile.Close()
		return DaemonEndpoint{}, err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Env = os.Environ()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureDetachedProcess(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return DaemonEndpoint{}, fmt.Errorf("start background daemon: %w", err)
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ep, _, ok := c.probeEndpoint(); ok {
			return ep, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return DaemonEndpoint{}, fmt.Errorf("timed out waiting for whatsapp-mcp daemon in %s", c.cfg.DataDir)
}

func (c *daemonClient) doWithEndpoint(ctx context.Context, ep DaemonEndpoint, method, path string, in any, out any) error {
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(dialCtx, ep.Network, ep.Address)
		},
	}
	defer transport.CloseIdleConnections()

	httpCli := &http.Client{
		Transport: transport,
		Timeout:   c.cfg.RequestTimeout,
	}

	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://whatsapp-mcp.local"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var errObj map[string]any
		if json.Unmarshal(respBytes, &errObj) == nil && errObj["error"] != nil {
			return fmt.Errorf("%v", errObj["error"])
		}
		return fmt.Errorf("daemon HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	if out != nil && len(respBytes) > 0 {
		return json.Unmarshal(respBytes, out)
	}
	return nil
}

func (c *daemonClient) call(ctx context.Context, method, path string, in any, out any) error {
	ep, err := c.ensureRunning()
	if err != nil {
		return err
	}
	return c.doWithEndpoint(ctx, ep, method, path, in, out)
}

func (c *daemonClient) stopIfRunning(ctx context.Context) (bool, error) {
	ep, _, ok := c.probeEndpoint()
	if !ok {
		return false, nil
	}
	var out map[string]any
	err := c.doWithEndpoint(ctx, ep, http.MethodPost, "/stop", nil, &out)
	return true, err
}
