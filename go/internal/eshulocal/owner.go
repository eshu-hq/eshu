package eshulocal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OwnerRecord captures the current workspace owner contract.
type OwnerRecord struct {
	PID                int    `json:"pid"`
	StartedAt          string `json:"started_at"`
	Hostname           string `json:"hostname"`
	WorkspaceID        string `json:"workspace_id"`
	Version            string `json:"version"`
	SocketPath         string `json:"socket_path"`
	PostgresPID        int    `json:"postgres_pid"`
	PostgresPort       int    `json:"postgres_port"`
	PostgresDataDir    string `json:"postgres_data_dir"`
	PostgresSocketDir  string `json:"postgres_socket_dir"`
	PostgresSocketPath string `json:"postgres_socket_path"`
	Profile            string `json:"profile,omitempty"`
	GraphBackend       string `json:"graph_backend,omitempty"`
	GraphAddress       string `json:"graph_address,omitempty"`
	GraphPID           int    `json:"graph_pid,omitempty"`
	GraphBoltPort      int    `json:"graph_bolt_port,omitempty"`
	GraphHTTPPort      int    `json:"graph_http_port,omitempty"`
	GraphDataDir       string `json:"graph_data_dir,omitempty"`
	GraphSocketPath    string `json:"graph_socket_path,omitempty"`
	GraphVersion       string `json:"graph_version,omitempty"`
	GraphUsername      string `json:"graph_username,omitempty"`
	GraphPassword      string `json:"graph_password,omitempty"`
}

// ReadOwnerRecord loads owner metadata from disk.
func ReadOwnerRecord(path string) (OwnerRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return OwnerRecord{}, fmt.Errorf("read owner record: %w", err)
	}

	var record OwnerRecord
	if err := json.Unmarshal(content, &record); err != nil {
		return OwnerRecord{}, fmt.Errorf("decode owner record: %w", err)
	}
	return record, nil
}

// WriteOwnerRecord atomically persists owner metadata to disk.
func WriteOwnerRecord(path string, record OwnerRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create owner record directory: %w", err)
	}

	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode owner record: %w", err)
	}
	content = append(content, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create owner record temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write owner record temp file: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod owner record temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync owner record temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close owner record temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace owner record: %w", err)
	}
	return nil
}
