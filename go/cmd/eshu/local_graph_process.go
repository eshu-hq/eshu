package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func startProcessLocalNornicDB(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
	binaryPath, err := resolveNornicDBBinary()
	if err != nil {
		return nil, err
	}
	version, err := readLocalGraphVersion(binaryPath)
	if err != nil {
		return nil, err
	}

	boltPort, err := reserveLocalGraphPort()
	if err != nil {
		return nil, err
	}
	httpPort, err := reserveLocalGraphPort()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(layout.GraphDir, "nornicdb")
	logPath := filepath.Join(layout.LogsDir, "graph-nornicdb.log")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create graph data directory: %w", err)
	}
	if err := os.MkdirAll(layout.LogsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create graph logs directory: %w", err)
	}
	credentials, err := loadOrCreateLocalGraphCredentials(filepath.Join(dataDir, "eshu-credentials.json"))
	if err != nil {
		return nil, err
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open graph log file: %w", err)
	}

	args := []string{
		"serve",
		"--headless",
		"--mcp-enabled=false",
		"--address=" + localNornicDBBindAddress,
		fmt.Sprintf("--bolt-port=%d", boltPort),
		fmt.Sprintf("--http-port=%d", httpPort),
		"--data-dir=" + dataDir,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Args = append([]string{binaryPath}, args...)
	cmd.Env = mergeEnvironment(eshuEnviron(), map[string]string{
		"NORNICDB_ADDRESS":          localNornicDBBindAddress,
		"NORNICDB_BOLT_PORT":        fmt.Sprintf("%d", boltPort),
		"NORNICDB_HTTP_PORT":        fmt.Sprintf("%d", httpPort),
		"NORNICDB_DATA_DIR":         dataDir,
		"NORNICDB_MCP_ENABLED":      "false",
		"NORNICDB_HEADLESS":         "true",
		"NORNICDB_AUTH":             credentials.Username + "/" + credentials.Password,
		"NORNICDB_DEFAULT_DATABASE": localNornicDBDefaultDatabase,
	})
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start nornicdb: %w", err)
	}

	graph := &managedLocalGraph{
		Backend:    query.GraphBackendNornicDB,
		Version:    version,
		BinaryPath: binaryPath,
		Address:    localNornicDBBindAddress,
		BoltPort:   boltPort,
		HTTPPort:   httpPort,
		DataDir:    dataDir,
		LogPath:    logPath,
		Username:   credentials.Username,
		Password:   credentials.Password,
		PID:        cmd.Process.Pid,
		Cmd:        cmd,
		logFile:    logFile,
	}
	if err := waitForManagedLocalGraph(ctx, graph, localGraphStartupTimeout); err != nil {
		_ = stopManagedLocalGraph(graph, localGraphShutdownTimeout)
		return nil, err
	}
	return graph, nil
}

func resolveNornicDBBinary() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("ESHU_NORNICDB_BINARY")); raw != "" {
		if _, err := localGraphReadVersion(raw); err != nil {
			return "", fmt.Errorf("verify nornicdb binary %q: %w", raw, err)
		}
		return raw, nil
	}
	if binaryPath, err := managedNornicDBBinaryIfPresent(); err == nil {
		return binaryPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	var verifyErrs []error
	for _, name := range []string{"nornicdb-headless", "nornicdb"} {
		binaryPath, err := localGraphLookPath(name)
		if err == nil {
			if _, err := localGraphReadVersion(binaryPath); err != nil {
				verifyErrs = append(verifyErrs, fmt.Errorf("%s at %q: %w", name, binaryPath, err))
				continue
			}
			return binaryPath, nil
		}
	}
	if len(verifyErrs) > 0 {
		return "", fmt.Errorf("resolve nornicdb binary: discovered candidate binaries but none passed version verification: %v", verifyErrs)
	}
	return "", fmt.Errorf("resolve nornicdb binary: set ESHU_NORNICDB_BINARY or place nornicdb-headless in PATH")
}

func readLocalGraphVersion(binaryPath string) (string, error) {
	output, err := exec.Command(binaryPath, "version").Output()
	if err != nil {
		return "", fmt.Errorf("read nornicdb version: %w", err)
	}
	return parseNornicDBVersionOutput(string(output))
}

func parseNornicDBVersionOutput(output string) (string, error) {
	version := strings.TrimSpace(output)
	if version == "" {
		return "", fmt.Errorf("empty output")
	}
	const prefix = "NornicDB "
	if !strings.HasPrefix(version, prefix) {
		return "", fmt.Errorf("unexpected output %q", version)
	}
	version = strings.TrimSpace(strings.TrimPrefix(version, prefix))
	if version == "" {
		return "", fmt.Errorf("missing version in output %q", output)
	}
	return version, nil
}
