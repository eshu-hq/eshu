// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	// mcpSessionAPIBindAddress is the loopback address every session eshu-api
	// binds. A session serves a single local MCP client, so it must never be
	// reachable off the machine.
	mcpSessionAPIBindAddress = "127.0.0.1"
	// mcpSessionStartupTimeout bounds the wait for a session eshu-api
	// /healthz.
	mcpSessionStartupTimeout = 2 * time.Minute
	// mcpSessionPollInterval is how often the session readiness wait re-checks.
	mcpSessionPollInterval = 200 * time.Millisecond
	// mcpBridgeMaxLineBytes caps one stdin JSON-RPC line. MCP requests are
	// small (initialize, tools/list, tools/call with arguments); the cap
	// fails a pathological line loudly instead of growing a buffer forever.
	mcpBridgeMaxLineBytes = 4 * 1024 * 1024
	// mcpBridgeMaxBodyBytes caps one satellite response body held in memory.
	mcpBridgeMaxBodyBytes = 32 * 1024 * 1024
	// mcpBridgeRequestTimeout bounds one bridge POST. It is a socket-leak
	// guard, not a query budget: graph-backed calls on a warm owner answer
	// far inside it, and a stuck satellite surfaces as an error instead of
	// hanging the client with no response line.
	mcpBridgeRequestTimeout = 5 * time.Minute
)

// MCPSession is a running MCP session: a loopback eshu-api child that serves
// the workspace stores plus the satellite eshu-mcp-server child that answers
// MCP clients against it. Whoever serves an MCP session spawns one: the
// owner-booted stdio path, the attach-stdio path, and the owner-attached HTTP
// path each own their session and tear it down when the session ends. The
// satellite holds no Postgres pool; the session eshu-api does.
type MCPSession struct {
	apiCmd              *exec.Cmd
	satelliteCmd        *exec.Cmd
	apiBaseURL          string
	satelliteMessageURL string
	apiKey              string
}

// StartMCPSession spawns a session eshu-api on a kernel-reserved loopback
// port, waits for its /healthz, then spawns the satellite eshu-mcp-server on
// a second reserved loopback port against it. A failure after a child has
// started stops what it started, so a failed session never leaves a child
// behind.
func StartMCPSession(ctx context.Context, dsn string, runtimeConfig RuntimeConfig, graph *ManagedGraph, layout eshulocal.Layout) (*MCPSession, error) {
	apiCmd, baseURL, err := startMCPSessionAPI(ctx, dsn, runtimeConfig, graph, layout)
	if err != nil {
		return nil, err
	}
	mcpPort, err := mcpSessionReservePort()
	if err != nil {
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
		return nil, err
	}
	satelliteAddr := net.JoinHostPort(mcpSessionAPIBindAddress, strconv.Itoa(mcpPort))
	satelliteEnv := ChildEnv(dsn, runtimeConfig, graph, mcpSatelliteSessionEnv(layout, runtimeConfig, mcpSessionAPIBindAddress))
	satelliteCmd, err := StartChildProcess("eshu-mcp-server",
		[]string{"eshu-mcp-server", "-addr=" + satelliteAddr, "-core-base-url=" + baseURL}, satelliteEnv)
	if err != nil {
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
		return nil, err
	}
	// The bridge forwards the first stdin line as soon as the session
	// returns, the way the old in-process server answered immediately. A
	// client sends initialize within milliseconds of spawn, so the satellite
	// must be serving before the session is handed over — otherwise the
	// first real request fails while the satellite is still booting.
	if err := mcpSessionWaitHTTP(ctx, "http://"+satelliteAddr+"/health", mcpSessionStartupTimeout); err != nil {
		_ = StopChildProcess(satelliteCmd, ShutdownTimeout)
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
		return nil, err
	}
	return &MCPSession{
		apiCmd:              apiCmd,
		satelliteCmd:        satelliteCmd,
		apiBaseURL:          baseURL,
		satelliteMessageURL: "http://" + satelliteAddr + "/mcp/message",
		apiKey:              strings.TrimSpace(os.Getenv("ESHU_API_KEY")),
	}, nil
}

// startMCPSessionAPI spawns one session eshu-api on a reserved loopback port
// and waits for it to answer /healthz. It stops the child when the wait
// fails, so callers only own a child on success.
func startMCPSessionAPI(ctx context.Context, dsn string, runtimeConfig RuntimeConfig, graph *ManagedGraph, layout eshulocal.Layout) (*exec.Cmd, string, error) {
	apiPort, err := mcpSessionReservePort()
	if err != nil {
		return nil, "", err
	}
	apiAddr := net.JoinHostPort(mcpSessionAPIBindAddress, strconv.Itoa(apiPort))
	apiEnv := ChildEnv(dsn, runtimeConfig, graph, ChildOverrides(layout, map[string]string{
		"ESHU_API_ADDR": apiAddr,
	}, os.Getenv))
	apiCmd, err := StartChildProcess("eshu-api", []string{"eshu-api"}, apiEnv)
	if err != nil {
		return nil, "", err
	}
	baseURL := "http://" + apiAddr
	if err := mcpSessionWaitHTTP(ctx, baseURL+"/healthz", mcpSessionStartupTimeout); err != nil {
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
		return nil, "", err
	}
	return apiCmd, baseURL, nil
}

// mcpSatelliteSessionEnv builds the satellite overrides for a session: HTTP
// transport, the session runtime profile, file logs, and the loopback-gated
// unauthenticated override. The caller layers it through ChildEnv, so the
// operator environment (ESHU_API_KEY, ESHU_MCP_RESULT_SHAPE,
// ESHU_MCP_TOOL_SURFACE) flows to the satellite untouched.
func mcpSatelliteSessionEnv(layout eshulocal.Layout, runtimeConfig RuntimeConfig, bindHost string) map[string]string {
	overrides := map[string]string{
		"ESHU_MCP_TRANSPORT":     "http",
		"ESHU_MCP_QUERY_PROFILE": string(runtimeConfig.Profile),
	}
	for key, value := range MCPHTTPAllowUnauthenticatedOverride(bindHost) {
		overrides[key] = value
	}
	return ChildOverrides(layout, overrides, os.Getenv)
}

// Children returns the session children for supervision alongside the owner's
// own children.
func (s *MCPSession) Children() []Child {
	return []Child{{name: "eshu-api", cmd: s.apiCmd}, {name: "eshu-mcp-server", cmd: s.satelliteCmd}}
}

// APIBaseURL returns the session eshu-api origin the satellite reads through.
func (s *MCPSession) APIBaseURL() string {
	return s.apiBaseURL
}

// SatelliteMessageURL returns the satellite POST /mcp/message URL the bridge
// forwards stdio lines to.
func (s *MCPSession) SatelliteMessageURL() string {
	return s.satelliteMessageURL
}

// ServeStdio forwards newline-delimited JSON-RPC from stdin to the session
// satellite and writes each response body as one stdout line. EOF on stdin
// ends the session.
func (s *MCPSession) ServeStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return RunMCPBridge(ctx, s.satelliteMessageURL, s.apiKey, stdin, stdout)
}

// Close stops the satellite, then the session API, joining the errors so one
// failing stop does not hide the other.
func (s *MCPSession) Close() error {
	return errors.Join(
		StopChildProcess(s.satelliteCmd, ShutdownTimeout),
		StopChildProcess(s.apiCmd, ShutdownTimeout),
	)
}

// runMCPStdioBridge runs a session's stdio bridge. It is a variable so owner
// and attach tests can prove the wiring without owning the process stdio.
var runMCPStdioBridge = func(ctx context.Context, session *MCPSession, stdin io.Reader, stdout io.Writer) error {
	return session.ServeStdio(ctx, stdin, stdout)
}

// RunMCPBridge forwards newline-delimited JSON-RPC from stdin to the satellite
// POST /mcp/message URL and writes each response body as one stdout line. An
// HTTP 204 (notification) emits nothing, and so does an empty body. EOF on
// stdin ends the bridge. A failed request with an id answers with a JSON-RPC
// error carrying that id instead of hanging the client with no response line;
// a failed notification fails the session loudly, because silence would hide
// the failure from a client that expects no answer.
func RunMCPBridge(ctx context.Context, satelliteMessageURL, apiKey string, stdin io.Reader, stdout io.Writer) error {
	client := &http.Client{Timeout: mcpBridgeRequestTimeout}
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), mcpBridgeMaxLineBytes)

	type bridgeLine struct {
		text string
	}
	lines := make(chan bridgeLine, 16)
	scanErr := make(chan error, 1)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			lines <- bridgeLine{text: scanner.Text()}
		}
		scanErr <- scanner.Err()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-lines:
			if !ok {
				if err := <-scanErr; err != nil {
					return fmt.Errorf("read MCP stdio: %w", err)
				}
				return nil
			}
			if strings.TrimSpace(line.text) == "" {
				continue
			}
			if err := postBridgeLine(ctx, client, satelliteMessageURL, apiKey, line.text, stdout); err != nil {
				return err
			}
		}
	}
}

// postBridgeLine sends one stdin line to the satellite and writes the answer.
// The transport framing stays here; admission stays in core.
func postBridgeLine(ctx context.Context, client *http.Client, satelliteMessageURL, apiKey, line string, stdout io.Writer) error {
	id, hasID := bridgeRequestID(line)
	fail := func(err error) error {
		if !hasID {
			return fmt.Errorf("post MCP message: %w", err)
		}
		return writeBridgeError(stdout, id, err.Error())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, satelliteMessageURL, strings.NewReader(line))
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, mcpBridgeMaxBodyBytes))
	if err != nil {
		return fail(err)
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode/100 != 2 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		if !hasID {
			return fmt.Errorf("post MCP message: satellite answered %d: %s", resp.StatusCode, snippet)
		}
		return writeBridgeError(stdout, id, fmt.Sprintf("satellite answered %d: %s", resp.StatusCode, snippet))
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		if !hasID {
			return nil
		}
		return writeBridgeError(stdout, id, fmt.Sprintf("satellite answered %d with an empty body", resp.StatusCode))
	}
	if _, err := io.WriteString(stdout, trimmed+"\n"); err != nil {
		return fmt.Errorf("write MCP stdio: %w", err)
	}
	return nil
}

// bridgeRequestID extracts the raw JSON-RPC id of one request line. A line
// without an id is a notification (or garbage); only an id can route an
// answer back to the waiting client.
func bridgeRequestID(line string) (json.RawMessage, bool) {
	var msg struct {
		ID *json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg.ID == nil {
		return nil, false
	}
	return *msg.ID, true
}

// writeBridgeError answers a failed request with a JSON-RPC error carrying
// its id, so the client unblocks with an error instead of hanging.
func writeBridgeError(stdout io.Writer, id json.RawMessage, message string) error {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	encoded, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32603,
			"message": "mcp bridge: " + message,
		},
	})
	if err != nil {
		return fmt.Errorf("encode MCP bridge error: %w", err)
	}
	if _, err := io.WriteString(stdout, string(encoded)+"\n"); err != nil {
		return fmt.Errorf("write MCP stdio: %w", err)
	}
	return nil
}

// RunAttachedMCPHTTP attaches an HTTP MCP endpoint to the workspace's running
// local owner: it spawns a session eshu-api plus the satellite at the user's
// host:port and waits on both, so Ctrl-C stops both. It refuses when no
// healthy owner holds the workspace rather than starting a second one.
func RunAttachedMCPHTTP(ctx context.Context, layout eshulocal.Layout, host string, port int) error {
	record, err := ReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no running local Eshu service owner for workspace %q; start it with eshu graph start --workspace-root %q", layout.WorkspaceRoot, layout.WorkspaceRoot)
		}
		return err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return fmt.Errorf("owner record workspace %q does not match requested workspace %q", record.WorkspaceID, layout.WorkspaceID)
	}
	if !ProcessAlive(record.PID) {
		return fmt.Errorf("no running local Eshu service owner for workspace %q; recorded owner pid %d is not alive", layout.WorkspaceRoot, record.PID)
	}
	if !SocketHealthy(record.PostgresSocketPath) {
		return fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy Postgres socket", layout.WorkspaceRoot)
	}
	if record.PostgresPort <= 0 {
		return fmt.Errorf("owner record for workspace %q missing postgres_port", layout.WorkspaceRoot)
	}

	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return err
	}
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && !GraphHealthy(record) {
		return fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy graph backend", layout.WorkspaceRoot)
	}

	dsn := eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort)
	apiCmd, baseURL, err := startMCPSessionAPI(ctx, dsn, runtimeConfig, ManagedGraphFromRecord(record), layout)
	if err != nil {
		return err
	}
	defer func() {
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
	}()

	satelliteAddr := net.JoinHostPort(host, strconv.Itoa(port))
	satelliteEnv := ChildEnv(dsn, runtimeConfig, ManagedGraphFromRecord(record), mcpSatelliteSessionEnv(layout, runtimeConfig, host))
	satelliteCmd, err := StartChildProcess("eshu-mcp-server",
		[]string{"eshu-mcp-server", "-addr=" + satelliteAddr, "-core-base-url=" + baseURL}, satelliteEnv)
	if err != nil {
		return err
	}
	defer func() {
		_ = StopChildProcess(satelliteCmd, ShutdownTimeout)
	}()

	return WaitManagedChildren(ctx, []Child{
		{name: "eshu-api", cmd: apiCmd},
		{name: "eshu-mcp-server", cmd: satelliteCmd},
	}, "eshu-mcp-server")
}

// mcpSessionReservePort picks the free loopback port a session child binds.
// It is a variable so session tests prove the wiring without binding ports.
var mcpSessionReservePort = reserveMCPSessionLoopbackPort

// mcpSessionWaitHTTP polls a session child health URL until it answers 200
// or the timeout expires: the session eshu-api /healthz after its spawn, the
// satellite /health after its spawn. Transport errors are retried rather than
// returned, because the child is still starting when the first attempts fail.
// It is a variable so session tests prove the wiring without serving HTTP.
var mcpSessionWaitHTTP = waitMCPSessionHTTPReady

// reserveMCPSessionLoopbackPort asks the kernel for a free loopback port and
// releases it immediately, so a session child can bind it. The gap between
// release and bind is a race the session accepts: a collision surfaces as a
// failed child startup, not as a wrong answer.
func reserveMCPSessionLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(mcpSessionAPIBindAddress, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve MCP session port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("reserve MCP session port: invalid tcp address %T", listener.Addr())
	}
	return addr.Port, nil
}

// waitMCPSessionHTTPReady polls url until it answers 200 or the timeout
// expires. Transport errors are retried rather than returned, because the
// child is still starting when the first attempts fail.
func waitMCPSessionHTTPReady(ctx context.Context, url string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: GraphHealthTimeout}
	ticker := time.NewTicker(mcpSessionPollInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for MCP session child %s timed out after %s: %w", url, timeout, waitCtx.Err())
		case <-ticker.C:
		}
	}
}
