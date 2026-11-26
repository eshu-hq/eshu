// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// stubMCPSessionChildren replaces the child-spawning and network seams with
// doubles that bind no ports and spawn no processes. It returns the captured
// starts and waits, and fails the test on unexpected interaction.
func stubMCPSessionChildren(t *testing.T, apiPort, mcpPort int) (started *[]childStart, waited *[]string) {
	t.Helper()

	originalStartChild := StartChildProcess
	originalReserve := mcpSessionReservePort
	originalWait := mcpSessionWaitHTTP
	t.Cleanup(func() {
		StartChildProcess = originalStartChild
		mcpSessionReservePort = originalReserve
		mcpSessionWaitHTTP = originalWait
	})

	starts := &[]childStart{}
	ports := []int{apiPort, mcpPort}
	mcpSessionReservePort = func() (int, error) {
		if len(ports) == 0 {
			t.Fatal("reserve port called more than twice")
			return 0, errors.New("no more ports")
		}
		port := ports[0]
		ports = ports[1:]
		return port, nil
	}

	waits := &[]string{}
	mcpSessionWaitHTTP = func(ctx context.Context, url string, timeout time.Duration) error {
		*waits = append(*waits, url)
		return nil
	}

	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		*starts = append(*starts, childStart{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
		return &exec.Cmd{}, nil
	}
	return starts, waits
}

// childStart records one StartChildProcess call.
type childStart struct {
	name string
	args []string
	env  []string
}

func TestStartMCPSessionSpawnsCoreThenSatellite(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "")
	starts, waits := stubMCPSessionChildren(t, 19191, 19192)

	runtimeConfig := RuntimeConfig{Profile: querycontract.ProfileLocalAuthoritative, GraphBackend: querycontract.GraphBackendNeo4j}
	layout := eshulocal.Layout{LogsDir: t.TempDir()}
	session, err := StartMCPSession(context.Background(), "host=127.0.0.1 port=15439 user=eshu dbname=postgres", runtimeConfig, nil, layout)
	if err != nil {
		t.Fatalf("StartMCPSession() error = %v, want nil", err)
	}
	if session == nil {
		t.Fatal("StartMCPSession() = nil, want session")
	}

	if len(*starts) != 2 {
		t.Fatalf("started %d children, want 2 (eshu-core-api, eshu-mcp-server)", len(*starts))
	}
	core := (*starts)[0]
	if core.name != "eshu-core-api" {
		t.Fatalf("first child = %q, want eshu-core-api", core.name)
	}
	assertArgEquals(t, core.args, "-addr", "127.0.0.1:19191")
	if got := localHostEnvValue(core.env, "ESHU_API_ADDR"); got != "" {
		t.Fatalf("ESHU_API_ADDR = %q, want unset (core takes -addr, reads no ESHU_API_ADDR)", got)
	}

	satellite := (*starts)[1]
	if satellite.name != "eshu-mcp-server" {
		t.Fatalf("second child = %q, want eshu-mcp-server", satellite.name)
	}
	assertArgEquals(t, satellite.args, "-addr", "127.0.0.1:19192")
	assertArgEquals(t, satellite.args, "-core-base-url", "http://127.0.0.1:19191")
	if got := localHostEnvValue(satellite.env, "ESHU_MCP_TRANSPORT"); got != "http" {
		t.Fatalf("ESHU_MCP_TRANSPORT = %q, want http", got)
	}
	if got := localHostEnvValue(satellite.env, "ESHU_MCP_QUERY_PROFILE"); got != string(querycontract.ProfileLocalAuthoritative) {
		t.Fatalf("ESHU_MCP_QUERY_PROFILE = %q, want local_authoritative", got)
	}
	if got := localHostEnvValue(satellite.env, "ESHU_MCP_ADDR"); got != "" {
		t.Fatalf("ESHU_MCP_ADDR = %q, want unset (listen address travels on -addr only)", got)
	}
	// Loopback session binds take the dev escape hatch through the same
	// loopback-gated override the CLI HTTP path uses.
	if got := localHostEnvValue(satellite.env, "ESHU_MCP_ALLOW_UNAUTHENTICATED"); got != "true" {
		t.Fatalf("ESHU_MCP_ALLOW_UNAUTHENTICATED = %q, want true for loopback session", got)
	}

	wantWaits := []string{"http://127.0.0.1:19191/health", "http://127.0.0.1:19192/health"}
	if strings.Join(*waits, ",") != strings.Join(wantWaits, ",") {
		t.Fatalf("waited on %v, want %v", *waits, wantWaits)
	}
	if got := session.APIBaseURL(); got != "http://127.0.0.1:19191" {
		t.Fatalf("APIBaseURL() = %q, want http://127.0.0.1:19191", got)
	}
	if got := session.SatelliteMessageURL(); got != "http://127.0.0.1:19192/mcp/message" {
		t.Fatalf("SatelliteMessageURL() = %q, want http://127.0.0.1:19192/mcp/message", got)
	}
	children := session.Children()
	if len(children) != 2 || children[0].name != "eshu-core-api" || children[1].name != "eshu-mcp-server" {
		t.Fatalf("Children() names = %v, want [eshu-core-api eshu-mcp-server]", children)
	}
}

// TestStartMCPSessionSelectsSessionAPIBackend proves the session API binary
// follows the graph backend: eshu-core-api on Neo4j (address on -addr, waits
// /health), eshu-local-api on NornicDB or an unset backend (address on
// ESHU_API_ADDR, waits /healthz). The satellite still points at the session
// base with -core-base-url regardless of which API serves it, and
// Children() carries the spawned API name.
func TestStartMCPSessionSelectsSessionAPIBackend(t *testing.T) {
	tests := []struct {
		name          string
		runtimeConfig RuntimeConfig
		wantName      string
		wantAddrFlag  string
		wantEnvAddr   string
		wantWaitPath  string
	}{
		{
			name:          "neo4j",
			runtimeConfig: RuntimeConfig{Profile: querycontract.ProfileLocalAuthoritative, GraphBackend: querycontract.GraphBackendNeo4j},
			wantName:      "eshu-core-api",
			wantAddrFlag:  "127.0.0.1:19191",
			wantEnvAddr:   "",
			wantWaitPath:  "/health",
		},
		{
			name:          "nornicdb",
			runtimeConfig: RuntimeConfig{Profile: querycontract.ProfileLocalAuthoritative, GraphBackend: querycontract.GraphBackendNornicDB},
			wantName:      "eshu-local-api",
			wantAddrFlag:  "",
			wantEnvAddr:   "127.0.0.1:19191",
			wantWaitPath:  "/healthz",
		},
		{
			name:          "lightweight",
			runtimeConfig: RuntimeConfig{Profile: querycontract.ProfileLocalLightweight},
			wantName:      "eshu-local-api",
			wantAddrFlag:  "",
			wantEnvAddr:   "127.0.0.1:19191",
			wantWaitPath:  "/healthz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ESHU_API_KEY", "")
			starts, waits := stubMCPSessionChildren(t, 19191, 19192)

			layout := eshulocal.Layout{LogsDir: t.TempDir()}
			session, err := StartMCPSession(context.Background(), "host=127.0.0.1 port=15439 user=eshu dbname=postgres", tt.runtimeConfig, nil, layout)
			if err != nil {
				t.Fatalf("StartMCPSession() error = %v, want nil", err)
			}
			if session == nil {
				t.Fatal("StartMCPSession() = nil, want session")
			}
			if len(*starts) != 2 {
				t.Fatalf("started %d children, want 2 (%s, eshu-mcp-server)", len(*starts), tt.wantName)
			}

			api := (*starts)[0]
			if api.name != tt.wantName {
				t.Fatalf("first child = %q, want %s", api.name, tt.wantName)
			}
			if tt.wantAddrFlag == "" {
				if len(api.args) != 1 || api.args[0] != tt.wantName {
					t.Fatalf("api argv = %#v, want [%q]", api.args, tt.wantName)
				}
			} else {
				assertArgEquals(t, api.args, "-addr", tt.wantAddrFlag)
			}
			if got := localHostEnvValue(api.env, "ESHU_API_ADDR"); got != tt.wantEnvAddr {
				t.Fatalf("ESHU_API_ADDR = %q, want %q", got, tt.wantEnvAddr)
			}

			satellite := (*starts)[1]
			if satellite.name != "eshu-mcp-server" {
				t.Fatalf("second child = %q, want eshu-mcp-server", satellite.name)
			}
			assertArgEquals(t, satellite.args, "-core-base-url", "http://127.0.0.1:19191")

			wantWaits := []string{"http://127.0.0.1:19191" + tt.wantWaitPath, "http://127.0.0.1:19192/health"}
			if strings.Join(*waits, ",") != strings.Join(wantWaits, ",") {
				t.Fatalf("waited on %v, want %v", *waits, wantWaits)
			}

			children := session.Children()
			if len(children) != 2 {
				t.Fatalf("Children() has %d entries, want 2", len(children))
			}
			if children[0].name != tt.wantName {
				t.Fatalf("Children()[0].name = %q, want %s", children[0].name, tt.wantName)
			}
			if children[1].name != "eshu-mcp-server" {
				t.Fatalf("Children()[1].name = %q, want eshu-mcp-server", children[1].name)
			}
		})
	}
}

// TestStartMCPSessionRefusesUnknownBackend proves any other backend value
// keeps the pre-spawn refusal: zero ports, zero children, and an error naming
// the backend.
func TestStartMCPSessionRefusesUnknownBackend(t *testing.T) {
	originalStartChild := StartChildProcess
	originalReserve := mcpSessionReservePort
	originalWait := mcpSessionWaitHTTP
	t.Cleanup(func() {
		StartChildProcess = originalStartChild
		mcpSessionReservePort = originalReserve
		mcpSessionWaitHTTP = originalWait
	})
	StartChildProcess = func(string, []string, []string) (*exec.Cmd, error) {
		t.Error("StartChildProcess called, want pre-spawn refusal")
		return nil, nil
	}
	mcpSessionReservePort = func() (int, error) {
		t.Error("mcpSessionReservePort called, want pre-spawn refusal")
		return 0, errors.New("no ports on a refused session")
	}
	mcpSessionWaitHTTP = func(context.Context, string, time.Duration) error {
		t.Error("mcpSessionWaitHTTP called, want pre-spawn refusal")
		return nil
	}

	layout := eshulocal.Layout{LogsDir: t.TempDir()}
	runtimeConfig := RuntimeConfig{Profile: querycontract.ProfileLocalAuthoritative, GraphBackend: "titan"}
	if _, err := StartMCPSession(context.Background(), "dsn", runtimeConfig, nil, layout); err == nil {
		t.Fatal("StartMCPSession() error = nil, want backend refusal")
	} else if !strings.Contains(err.Error(), `"titan"`) {
		t.Fatalf("StartMCPSession() error = %q, want it to name the backend", err)
	}
}

func assertArgEquals(t *testing.T, args []string, name, want string) {
	t.Helper()
	prefix := name + "="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			if got := strings.TrimPrefix(arg, prefix); got != want {
				t.Fatalf("%s = %q, want %q", name, got, want)
			}
			return
		}
	}
	t.Fatalf("args = %#v, want %s=%q", args, name, want)
}

// satelliteStandIn is an httptest double for the satellite POST /mcp/message
// surface: synchronous responses inline, 204 for notifications.
type satelliteStandIn struct {
	t        *testing.T
	mu       sync.Mutex
	methods  []string
	authSeen []string
}

func (s *satelliteStandIn) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp/message" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.methods = append(s.methods, req.Method)
		s.authSeen = append(s.authSeen, r.Header.Get("Authorization"))
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{"echo": req.Method},
		})
	}
}

// TestRunMCPBridgeForwardsRequestsAndSkipsNotifications proves the stdio
// framing contract: initialize, tools/list, and one tools/call each answer
// with one stdout line, while a notification (HTTP 204) emits nothing.
func TestRunMCPBridgeForwardsRequestsAndSkipsNotifications(t *testing.T) {
	standIn := &satelliteStandIn{t: t}
	server := httptest.NewServer(standIn.handler())
	defer server.Close()

	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_code","arguments":{"query":"x"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		"",
	}, "\n")
	var stdout strings.Builder
	if err := RunMCPBridge(context.Background(), server.URL+"/mcp/message", "test-key", strings.NewReader(stdin), &stdout); err != nil {
		t.Fatalf("RunMCPBridge() error = %v, want nil", err)
	}

	standIn.mu.Lock()
	defer standIn.mu.Unlock()
	wantMethods := []string{"initialize", "tools/list", "tools/call", "notifications/initialized"}
	if strings.Join(standIn.methods, ",") != strings.Join(wantMethods, ",") {
		t.Fatalf("satellite saw methods %v, want %v", standIn.methods, wantMethods)
	}
	for _, auth := range standIn.authSeen {
		if auth != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", auth)
		}
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout has %d lines, want 3 (notification emits nothing): %q", len(lines), stdout.String())
	}
	for i, line := range lines {
		var resp struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("stdout line %d is not one JSON object: %v", i, err)
		}
		if resp.JSONRPC != "2.0" || resp.ID != i+1 {
			t.Fatalf("stdout line %d = %q, want jsonrpc 2.0 response with id %d", i, line, i+1)
		}
	}
}

// TestRunMCPBridgeSynthesizesErrorOnBadStatusWithBody proves a non-2xx
// satellite answer carrying a body (a proxy HTML error page, not JSON-RPC)
// is converted into a JSON-RPC error with the request id instead of being
// relayed verbatim. Relaying it verbatim would hand the client an HTML line
// it must reject.
func TestRunMCPBridgeSynthesizesErrorOnBadStatusWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html><body>bad gateway</body></html>")
	}))
	defer server.Close()

	stdin := `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}` + "\n"
	var stdout strings.Builder
	if err := RunMCPBridge(context.Background(), server.URL+"/mcp/message", "test-key", strings.NewReader(stdin), &stdout); err != nil {
		t.Fatalf("RunMCPBridge() error = %v, want nil", err)
	}

	line := strings.TrimRight(stdout.String(), "\n")
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("stdout is not one JSON object: %v: %q", err, line)
	}
	if resp.JSONRPC != "2.0" || resp.ID != 7 || resp.Error.Code == 0 {
		t.Fatalf("stdout = %q, want JSON-RPC error with id 7", line)
	}
	if !strings.Contains(resp.Error.Message, "502") {
		t.Fatalf("error message = %q, want the satellite status", resp.Error.Message)
	}
	if strings.Contains(line, "<html>") {
		t.Fatalf("stdout relays the proxy HTML verbatim: %q", line)
	}
}

// TestRunMCPBridgeEOFEndsSession proves an empty stdin ends the bridge cleanly
// with no satellite traffic.
func TestRunMCPBridgeEOFEndsSession(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout strings.Builder
	if err := RunMCPBridge(context.Background(), server.URL+"/mcp/message", "", strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("RunMCPBridge() error = %v, want nil", err)
	}
	if calls != 0 {
		t.Fatalf("satellite got %d requests, want 0", calls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

// TestRunMCPBridgeSynthesizesErrorOnTransportFailure proves a dead satellite
// answers the pending request with a JSON-RPC error carrying its id instead
// of hanging the client with no response line.
func TestRunMCPBridgeSynthesizesErrorOnTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL + "/mcp/message"
	server.Close()

	var stdout strings.Builder
	stdin := `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}` + "\n"
	if err := RunMCPBridge(context.Background(), url, "", strings.NewReader(stdin), &stdout); err != nil {
		t.Fatalf("RunMCPBridge() error = %v, want nil (per-request failure stays on the line)", err)
	}
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &resp); err != nil {
		t.Fatalf("stdout = %q, want one JSON-RPC error object: %v", stdout.String(), err)
	}
	if resp.ID != 7 || resp.Error == nil {
		t.Fatalf("stdout = %q, want JSON-RPC error with id 7", stdout.String())
	}
}

func TestStartMCPSessionStopsCoreWhenSatelliteFails(t *testing.T) {
	starts, _ := stubMCPSessionChildren(t, 19191, 19192)

	wantErr := errors.New("satellite start sentinel")
	originalStartChild := StartChildProcess
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		*starts = append(*starts, childStart{name: name, args: args, env: env})
		if name == "eshu-mcp-server" {
			return nil, wantErr
		}
		return &exec.Cmd{}, nil
	}
	defer func() { StartChildProcess = originalStartChild }()

	runtimeConfig := RuntimeConfig{Profile: querycontract.ProfileLocalAuthoritative, GraphBackend: querycontract.GraphBackendNeo4j}
	layout := eshulocal.Layout{LogsDir: t.TempDir()}
	if _, err := StartMCPSession(context.Background(), "dsn", runtimeConfig, nil, layout); !errors.Is(err, wantErr) {
		t.Fatalf("StartMCPSession() error = %v, want %v", err, wantErr)
	}
}

func TestRunAttachedMCPHTTPRefusesWithoutHealthyOwner(t *testing.T) {
	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalStartChild := StartChildProcess
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		StartChildProcess = originalStartChild
	})
	ReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{}, errors.New("no record")
	}
	StartChildProcess = func(string, []string, []string) (*exec.Cmd, error) {
		t.Fatal("StartChildProcess called, want owner preflight error")
		return nil, nil
	}

	layout := eshulocal.Layout{WorkspaceID: "workspace-id", OwnerRecordPath: "/tmp/owner.json"}
	if err := RunAttachedMCPHTTP(context.Background(), layout, "127.0.0.1", 8081); err == nil {
		t.Fatal("RunAttachedMCPHTTP() error = nil, want owner error")
	}
}

func TestRunAttachedMCPHTTPStartsSessionChildrenAndWaits(t *testing.T) {
	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalGraphHealthy := GraphHealthy
	originalStartChild := StartChildProcess
	originalReserve := mcpSessionReservePort
	originalWait := mcpSessionWaitHTTP
	originalWaitManaged := WaitManagedChildren
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		GraphHealthy = originalGraphHealthy
		StartChildProcess = originalStartChild
		mcpSessionReservePort = originalReserve
		mcpSessionWaitHTTP = originalWait
		WaitManagedChildren = originalWaitManaged
	})

	record := eshulocal.OwnerRecord{
		PID:                42,
		WorkspaceID:        "workspace-id",
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            string(querycontract.ProfileLocalAuthoritative),
		GraphBackend:       string(querycontract.GraphBackendNeo4j),
	}
	ReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) { return record, nil }
	ProcessAlive = func(int) bool { return true }
	SocketHealthy = func(string) bool { return true }
	GraphHealthy = func(eshulocal.OwnerRecord) bool { return true }

	var starts []childStart
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		starts = append(starts, childStart{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
		return &exec.Cmd{}, nil
	}
	mcpSessionReservePort = func() (int, error) { return 19391, nil }
	var waitedURLs []string
	mcpSessionWaitHTTP = func(_ context.Context, url string, _ time.Duration) error {
		waitedURLs = append(waitedURLs, url)
		return nil
	}
	var waited []Child
	WaitManagedChildren = func(_ context.Context, children []Child, allowCleanExit string) error {
		waited = append([]Child(nil), children...)
		if allowCleanExit != "eshu-mcp-server" {
			t.Fatalf("allowCleanExit = %q, want eshu-mcp-server", allowCleanExit)
		}
		return nil
	}

	layout := eshulocal.Layout{WorkspaceID: "workspace-id", OwnerRecordPath: "/tmp/owner.json", LogsDir: t.TempDir()}
	if err := RunAttachedMCPHTTP(context.Background(), layout, "127.0.0.1", 8081); err != nil {
		t.Fatalf("RunAttachedMCPHTTP() error = %v, want nil", err)
	}

	if len(starts) != 2 || starts[0].name != "eshu-core-api" || starts[1].name != "eshu-mcp-server" {
		t.Fatalf("started = %v, want [eshu-core-api eshu-mcp-server]", starts)
	}
	assertArgEquals(t, starts[1].args, "-addr", "127.0.0.1:8081")
	assertArgEquals(t, starts[1].args, "-core-base-url", "http://127.0.0.1:19391")
	if len(waitedURLs) != 1 || waitedURLs[0] != "http://127.0.0.1:19391/health" {
		t.Fatalf("waited on %v, want [http://127.0.0.1:19391/health]", waitedURLs)
	}
	if len(waited) != 2 || waited[0].name != "eshu-core-api" || waited[1].name != "eshu-mcp-server" {
		t.Fatalf("waited children = %v, want [eshu-core-api eshu-mcp-server]", waited)
	}
	if got := localHostEnvValue(starts[1].env, "ESHU_POSTGRES_DSN"); !strings.Contains(got, "port=15439") {
		t.Fatalf("satellite ESHU_POSTGRES_DSN = %q, want recorded port", got)
	}
}

// TestRunAttachedMCPHTTPStartsLocalAPIFromNornicdbOwner proves the attached
// HTTP path follows the recorded backend too: a NornicDB owner gets an
// eshu-local-api session child (address on ESHU_API_ADDR, /healthz wait), and
// the supervised names travel with the spawned binary.
func TestRunAttachedMCPHTTPStartsLocalAPIFromNornicdbOwner(t *testing.T) {
	originalReadOwnerRecord := ReadOwnerRecord
	originalProcessAlive := ProcessAlive
	originalSocketHealthy := SocketHealthy
	originalGraphHealthy := GraphHealthy
	originalStartChild := StartChildProcess
	originalReserve := mcpSessionReservePort
	originalWait := mcpSessionWaitHTTP
	originalWaitManaged := WaitManagedChildren
	t.Cleanup(func() {
		ReadOwnerRecord = originalReadOwnerRecord
		ProcessAlive = originalProcessAlive
		SocketHealthy = originalSocketHealthy
		GraphHealthy = originalGraphHealthy
		StartChildProcess = originalStartChild
		mcpSessionReservePort = originalReserve
		mcpSessionWaitHTTP = originalWait
		WaitManagedChildren = originalWaitManaged
	})

	record := eshulocal.OwnerRecord{
		PID:                42,
		WorkspaceID:        "workspace-id",
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            string(querycontract.ProfileLocalAuthoritative),
		GraphBackend:       string(querycontract.GraphBackendNornicDB),
	}
	ReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) { return record, nil }
	ProcessAlive = func(int) bool { return true }
	SocketHealthy = func(string) bool { return true }
	GraphHealthy = func(eshulocal.OwnerRecord) bool { return true }

	var starts []childStart
	StartChildProcess = func(name string, args []string, env []string) (*exec.Cmd, error) {
		starts = append(starts, childStart{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
		return &exec.Cmd{}, nil
	}
	mcpSessionReservePort = func() (int, error) { return 19391, nil }
	var waitedURLs []string
	mcpSessionWaitHTTP = func(_ context.Context, url string, _ time.Duration) error {
		waitedURLs = append(waitedURLs, url)
		return nil
	}
	var waited []Child
	WaitManagedChildren = func(_ context.Context, children []Child, allowCleanExit string) error {
		waited = append([]Child(nil), children...)
		if allowCleanExit != "eshu-mcp-server" {
			t.Fatalf("allowCleanExit = %q, want eshu-mcp-server", allowCleanExit)
		}
		return nil
	}

	layout := eshulocal.Layout{WorkspaceID: "workspace-id", OwnerRecordPath: "/tmp/owner.json", LogsDir: t.TempDir()}
	if err := RunAttachedMCPHTTP(context.Background(), layout, "127.0.0.1", 8081); err != nil {
		t.Fatalf("RunAttachedMCPHTTP() error = %v, want nil", err)
	}

	if len(starts) != 2 || starts[0].name != "eshu-local-api" || starts[1].name != "eshu-mcp-server" {
		t.Fatalf("started = %v, want [eshu-local-api eshu-mcp-server]", starts)
	}
	if len(starts[0].args) != 1 || starts[0].args[0] != "eshu-local-api" {
		t.Fatalf("api argv = %#v, want [eshu-local-api]", starts[0].args)
	}
	if got := localHostEnvValue(starts[0].env, "ESHU_API_ADDR"); got != "127.0.0.1:19391" {
		t.Fatalf("api ESHU_API_ADDR = %q, want 127.0.0.1:19391", got)
	}
	assertArgEquals(t, starts[1].args, "-addr", "127.0.0.1:8081")
	assertArgEquals(t, starts[1].args, "-core-base-url", "http://127.0.0.1:19391")
	if len(waitedURLs) != 1 || waitedURLs[0] != "http://127.0.0.1:19391/healthz" {
		t.Fatalf("waited on %v, want [http://127.0.0.1:19391/healthz]", waitedURLs)
	}
	if len(waited) != 2 || waited[0].name != "eshu-local-api" || waited[1].name != "eshu-mcp-server" {
		t.Fatalf("waited children = %v, want [eshu-local-api eshu-mcp-server]", waited)
	}
}
