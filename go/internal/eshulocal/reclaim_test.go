package eshulocal

import (
	"errors"
	"os"
	"testing"
)

func TestValidateOrReclaimOwner(t *testing.T) {
	t.Run("missing owner record is already reclaimable", func(t *testing.T) {
		layout := testLayout(t)

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{})
		if err != nil {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want nil", err)
		}
	})

	t.Run("live owner pid blocks reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:         42,
			Version:     "v1",
			SocketPath:  "/tmp/eshu-owner.sock",
			PostgresPID: 77,
		}
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			PIDAlive: func(pid int) bool { return pid == 42 },
		})
		if !errors.Is(err, ErrWorkspaceOwnerActive) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrWorkspaceOwnerActive)
		}
		if _, err := os.Stat(layout.OwnerRecordPath); err != nil {
			t.Fatalf("Stat(owner record) error = %v, want nil", err)
		}
	})

	t.Run("healthy owner socket blocks reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:        42,
			Version:    "v1",
			SocketPath: "/tmp/eshu-owner.sock",
		}
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			SocketHealthy: func(path string) bool { return path == record.SocketPath },
		})
		if !errors.Is(err, ErrWorkspaceOwnerActive) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrWorkspaceOwnerActive)
		}
	})

	t.Run("stale owner with dead postgres is reclaimed", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:                42,
			Version:            "v0",
			WorkspaceID:        "workspace-id",
			SocketPath:         "/tmp/eshu-owner.sock",
			PostgresPID:        77,
			PostgresSocketPath: "/tmp/.s.PGSQL.15433",
			PostgresDataDir:    layout.PostgresDir,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{})
		if err != nil {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want nil", err)
		}
		if _, err := os.Stat(layout.OwnerRecordPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Stat(owner record) error = %v, want %v", err, os.ErrNotExist)
		}
	})

	t.Run("stale owner stops postgres before reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:                42,
			Version:            "v1",
			WorkspaceID:        "workspace-id",
			SocketPath:         "/tmp/eshu-owner.sock",
			PostgresPID:        77,
			PostgresSocketPath: "/tmp/.s.PGSQL.15433",
			PostgresDataDir:    layout.PostgresDir,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		pgAlive := true
		stopCalls := 0
		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			PIDAlive: func(pid int) bool { return pid == record.PostgresPID && pgAlive },
			StopPostgres: func(dataDir string) error {
				stopCalls++
				if dataDir != record.PostgresDataDir {
					t.Fatalf("StopPostgres() dataDir = %q, want %q", dataDir, record.PostgresDataDir)
				}
				pgAlive = false
				return nil
			},
		})
		if err != nil {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want nil", err)
		}
		if stopCalls != 1 {
			t.Fatalf("StopPostgres() call count = %d, want 1", stopCalls)
		}
	})

	t.Run("postgres stop failure blocks reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:             42,
			Version:         "v1",
			WorkspaceID:     "workspace-id",
			PostgresPID:     77,
			PostgresDataDir: layout.PostgresDir,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		wantErr := errors.New("pg_ctl failed")
		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			PIDAlive: func(pid int) bool { return pid == record.PostgresPID },
			StopPostgres: func(dataDir string) error {
				return wantErr
			},
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("postgres still healthy after stop blocks reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:                42,
			Version:            "v1",
			WorkspaceID:        "workspace-id",
			PostgresPID:        77,
			PostgresSocketPath: "/tmp/.s.PGSQL.15433",
			PostgresDataDir:    layout.PostgresDir,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		stopCalls := 0
		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			PIDAlive: func(pid int) bool { return pid == record.PostgresPID },
			StopPostgres: func(dataDir string) error {
				stopCalls++
				return nil
			},
			SocketHealthy: func(path string) bool { return false },
		})
		if !errors.Is(err, ErrEmbeddedPostgresActive) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrEmbeddedPostgresActive)
		}
		if stopCalls != 1 {
			t.Fatalf("StopPostgres() call count = %d, want 1", stopCalls)
		}
	})

	t.Run("stale graph stops before reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:           42,
			Version:       "v1",
			WorkspaceID:   "workspace-id",
			GraphPID:      88,
			GraphHTTPPort: 17474,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		graphAlive := true
		stopCalls := 0
		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			GraphHealthy: func(record OwnerRecord) bool {
				return graphAlive && record.GraphPID == 88
			},
			StopGraph: func(record OwnerRecord) error {
				stopCalls++
				if record.GraphPID != 88 {
					t.Fatalf("StopGraph() record.GraphPID = %d, want %d", record.GraphPID, 88)
				}
				graphAlive = false
				return nil
			},
		})
		if err != nil {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want nil", err)
		}
		if stopCalls != 1 {
			t.Fatalf("StopGraph() call count = %d, want 1", stopCalls)
		}
	})

	t.Run("graph still healthy after stop blocks reclaim", func(t *testing.T) {
		layout := testLayout(t)
		record := OwnerRecord{
			PID:           42,
			Version:       "v1",
			WorkspaceID:   "workspace-id",
			GraphPID:      88,
			GraphHTTPPort: 17474,
		}
		layout.WorkspaceID = "workspace-id"
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		stopCalls := 0
		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			GraphHealthy: func(record OwnerRecord) bool {
				return record.GraphPID == 88
			},
			StopGraph: func(record OwnerRecord) error {
				stopCalls++
				return nil
			},
		})
		if !errors.Is(err, ErrGraphBackendActive) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrGraphBackendActive)
		}
		if stopCalls != 1 {
			t.Fatalf("StopGraph() call count = %d, want 1", stopCalls)
		}
	})

	t.Run("workspace id mismatch fails closed", func(t *testing.T) {
		layout := testLayout(t)
		layout.WorkspaceID = "expected-workspace"
		record := OwnerRecord{
			WorkspaceID: "different-workspace",
		}
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{})
		if !errors.Is(err, ErrInvalidOwnerRecord) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrInvalidOwnerRecord)
		}
	})

	t.Run("live postgres without data dir fails closed", func(t *testing.T) {
		layout := testLayout(t)
		layout.WorkspaceID = "workspace-id"
		record := OwnerRecord{
			WorkspaceID: "workspace-id",
			PostgresPID: 77,
		}
		if err := WriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
			t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
		}

		err := ValidateOrReclaimOwner(layout, "v1", ReclaimDeps{
			PIDAlive: func(pid int) bool { return pid == 77 },
		})
		if !errors.Is(err, ErrInvalidOwnerRecord) {
			t.Fatalf("ValidateOrReclaimOwner() error = %v, want %v", err, ErrInvalidOwnerRecord)
		}
	})
}
