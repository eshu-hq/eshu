package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	localGraphStartupTimeout  = 45 * time.Second
	localGraphHealthTimeout   = 1 * time.Second
	localGraphShutdownTimeout = 10 * time.Second

	localNornicDBBindAddress     = "127.0.0.1"
	localNornicDBAdminUsername   = "admin"
	localNornicDBDefaultDatabase = "nornic"
	localNornicDBRuntimeModeEnv  = "ESHU_NORNICDB_RUNTIME"
)

var (
	localGraphLookPath          = exec.LookPath
	localGraphReadVersion       = readLocalGraphVersion
	localGraphGeneratePassword  = generateLocalGraphPassword
	localGraphHTTPHealthy       = graphHTTPHealthy
	localGraphBoltHealthy       = graphBoltHealthy
	localGraphStartEmbedded     = startEmbeddedLocalNornicDB
	localGraphStartProcess      = startProcessLocalNornicDB
	localGraphEmbeddedAvailable = embeddedLocalNornicDBAvailable
)

type managedLocalGraph struct {
	Backend    query.GraphBackend
	Version    string
	BinaryPath string
	Address    string
	BoltPort   int
	HTTPPort   int
	DataDir    string
	LogPath    string
	Username   string
	Password   string
	PID        int
	Cmd        *exec.Cmd
	logFile    io.Closer
	shutdown   func(context.Context) error
}

func startManagedLocalGraph(ctx context.Context, layout eshulocal.Layout, runtimeConfig localHostRuntimeConfig) (*managedLocalGraph, error) {
	switch runtimeConfig.GraphBackend {
	case query.GraphBackendNornicDB:
		return startManagedLocalNornicDB(ctx, layout)
	default:
		return nil, fmt.Errorf("graph backend %q is not supported by local_authoritative host", runtimeConfig.GraphBackend)
	}
}

func startManagedLocalNornicDB(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
	useProcess, err := useProcessLocalNornicDB(os.Getenv, localGraphEmbeddedAvailable())
	if err != nil {
		return nil, err
	}
	if useProcess {
		return localGraphStartProcess(ctx, layout)
	}
	return localGraphStartEmbedded(ctx, layout)
}

// useProcessLocalNornicDB decides whether local-authoritative graph ownership
// should spawn a managed process or use the in-process NornicDB runtime. Empty
// runtime mode means embedded; process mode is an explicit maintainer escape
// hatch for testing a patched backend.
func useProcessLocalNornicDB(getenv func(string) string, embeddedAvailable bool) (bool, error) {
	if strings.TrimSpace(getenv("ESHU_NORNICDB_BINARY")) != "" {
		return true, nil
	}
	switch mode := strings.ToLower(strings.TrimSpace(getenv(localNornicDBRuntimeModeEnv))); mode {
	case "", "embedded":
		if !embeddedAvailable {
			return false, fmt.Errorf("embedded NornicDB is not available in this Eshu build; rebuild with -tags nolocalllm or set %s=process", localNornicDBRuntimeModeEnv)
		}
		return false, nil
	case "process":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be embedded or process, got %q", localNornicDBRuntimeModeEnv, mode)
	}
}

func generateLocalGraphPassword() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate local graph password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

type localGraphCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadOrCreateLocalGraphCredentials(path string) (localGraphCredentials, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		var credentials localGraphCredentials
		if err := json.Unmarshal(content, &credentials); err != nil {
			return localGraphCredentials{}, fmt.Errorf("decode local graph credentials: %w", err)
		}
		if strings.TrimSpace(credentials.Username) == "" || strings.TrimSpace(credentials.Password) == "" {
			return localGraphCredentials{}, fmt.Errorf("decode local graph credentials: username and password are required")
		}
		return credentials, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return localGraphCredentials{}, fmt.Errorf("read local graph credentials: %w", err)
	}

	password, err := localGraphGeneratePassword()
	if err != nil {
		return localGraphCredentials{}, err
	}
	credentials := localGraphCredentials{
		Username: localNornicDBAdminUsername,
		Password: password,
	}
	if err := writeLocalGraphCredentials(path, credentials); err != nil {
		return localGraphCredentials{}, err
	}
	return credentials, nil
}

func writeLocalGraphCredentials(path string, credentials localGraphCredentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create local graph credentials directory: %w", err)
	}
	content, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local graph credentials: %w", err)
	}
	content = append(content, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create local graph credentials temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write local graph credentials temp file: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod local graph credentials temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync local graph credentials temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close local graph credentials temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace local graph credentials: %w", err)
	}
	return nil
}

func reserveLocalGraphPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(localNornicDBBindAddress, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve local graph port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("reserve local graph port: invalid tcp address %T", listener.Addr())
	}
	return addr.Port, nil
}

func waitForManagedLocalGraph(ctx context.Context, graph *managedLocalGraph, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if graphHealthyFromOwnerRecord(eshulocal.OwnerRecord{
			GraphPID:      graph.PID,
			GraphAddress:  graph.Address,
			GraphBoltPort: graph.BoltPort,
			GraphHTTPPort: graph.HTTPPort,
		}) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for graph backend readiness timed out after %s; see %s", timeout, graph.LogPath)
		}
		if graph.Cmd != nil && graph.Cmd.ProcessState != nil && graph.Cmd.ProcessState.Exited() {
			return fmt.Errorf("graph backend exited before becoming ready; see %s", graph.LogPath)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func stopManagedLocalGraph(graph *managedLocalGraph, timeout time.Duration) error {
	if graph == nil {
		return nil
	}
	defer func() {
		if graph.logFile != nil {
			_ = graph.logFile.Close()
		}
	}()

	if graph.shutdown != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return graph.shutdown(ctx)
	}

	if graph.Cmd == nil || graph.Cmd.Process == nil {
		return nil
	}
	if graph.Cmd.ProcessState != nil && graph.Cmd.ProcessState.Exited() {
		return nil
	}
	if err := graph.Cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = graph.Cmd.Process.Kill()
		return fmt.Errorf("signal graph backend: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- graph.Cmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
		return err
	case <-time.After(timeout):
		if err := graph.Cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill graph backend: %w", err)
		}
		<-done
		return nil
	}
}

func stopRecordedLocalGraph(record eshulocal.OwnerRecord) error {
	graph := managedGraphFromRecord(record)
	if graph == nil {
		return nil
	}
	process, err := os.FindProcess(graph.PID)
	if err != nil {
		return fmt.Errorf("find graph backend process: %w", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal graph backend: %w", err)
	}
	deadline := time.Now().Add(localGraphShutdownTimeout)
	for time.Now().Before(deadline) {
		if !graphHealthyFromOwnerRecord(record) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill graph backend: %w", err)
	}
	return nil
}

func managedGraphFromRecord(record eshulocal.OwnerRecord) *managedLocalGraph {
	if record.GraphBackend == "" {
		return nil
	}
	return &managedLocalGraph{
		Backend:    query.GraphBackend(record.GraphBackend),
		Version:    record.GraphVersion,
		Address:    record.GraphAddress,
		BoltPort:   record.GraphBoltPort,
		HTTPPort:   record.GraphHTTPPort,
		DataDir:    record.GraphDataDir,
		PID:        record.GraphPID,
		Username:   record.GraphUsername,
		Password:   record.GraphPassword,
		BinaryPath: "",
	}
}

func graphEnvOverrides(graph *managedLocalGraph) map[string]string {
	if graph == nil || graph.Backend != query.GraphBackendNornicDB {
		return nil
	}
	boltURI := fmt.Sprintf("bolt://%s:%d", graph.Address, graph.BoltPort)
	username := graph.Username
	if username == "" {
		username = localNornicDBAdminUsername
	}
	// Keep the ESHU-prefixed variables as canonical and NEO4J_* as legacy
	// compatibility for code paths that have not yet moved to ESHU_* names.
	return map[string]string{
		"ESHU_NEO4J_URI":      boltURI,
		"ESHU_NEO4J_USERNAME": username,
		"ESHU_NEO4J_PASSWORD": graph.Password,
		"ESHU_NEO4J_DATABASE": localNornicDBDefaultDatabase,
		"NEO4J_URI":           boltURI,
		"NEO4J_USERNAME":      username,
		"NEO4J_PASSWORD":      graph.Password,
		"NEO4J_DATABASE":      localNornicDBDefaultDatabase,
		"DEFAULT_DATABASE":    localNornicDBDefaultDatabase,
	}
}

func graphAddress(graph *managedLocalGraph) string {
	if graph == nil {
		return ""
	}
	return graph.Address
}

func graphPID(graph *managedLocalGraph) int {
	if graph == nil {
		return 0
	}
	return graph.PID
}

func graphBoltPort(graph *managedLocalGraph) int {
	if graph == nil {
		return 0
	}
	return graph.BoltPort
}

func graphHTTPPort(graph *managedLocalGraph) int {
	if graph == nil {
		return 0
	}
	return graph.HTTPPort
}

func graphDataDir(graph *managedLocalGraph) string {
	if graph == nil {
		return ""
	}
	return graph.DataDir
}

func graphVersion(graph *managedLocalGraph) string {
	if graph == nil {
		return ""
	}
	return graph.Version
}

func graphUsername(graph *managedLocalGraph) string {
	if graph == nil {
		return ""
	}
	return graph.Username
}

func graphPassword(graph *managedLocalGraph) string {
	if graph == nil {
		return ""
	}
	return graph.Password
}

func graphHealthyFromOwnerRecord(record eshulocal.OwnerRecord) bool {
	if record.GraphPID <= 0 {
		return false
	}
	address := strings.TrimSpace(record.GraphAddress)
	if address == "" {
		address = localNornicDBBindAddress
	}
	if record.GraphBoltPort <= 0 || record.GraphHTTPPort <= 0 {
		return false
	}
	return localHostProcessAlive(record.GraphPID) &&
		localGraphHTTPHealthy(address, record.GraphHTTPPort, localGraphHealthTimeout) &&
		localGraphBoltHealthy(address, record.GraphBoltPort, localGraphHealthTimeout)
}

func graphHTTPHealthy(address string, port int, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/health", address, port))
	if err != nil {
		return false
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	return resp.StatusCode == http.StatusOK
}

func graphBoltHealthy(address string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, fmt.Sprintf("%d", port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
