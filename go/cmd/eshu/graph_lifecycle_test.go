package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestGraphStopSignalsLiveOwnerInsteadOfGraphProcess(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalGraphHealthy := graphStopGraphHealthy
	originalSignalProcess := graphSignalProcess
	originalStopRecordedGraph := graphStopRecordedGraph
	originalStopPollInterval := graphStopPollInterval
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphStopGraphHealthy = originalGraphHealthy
		graphSignalProcess = originalSignalProcess
		graphStopRecordedGraph = originalStopRecordedGraph
		graphStopPollInterval = originalStopPollInterval
	})

	record := eshulocal.OwnerRecord{
		PID:           42,
		WorkspaceID:   "workspace-id",
		Profile:       string(query.ProfileLocalAuthoritative),
		GraphBackend:  string(query.GraphBackendNornicDB),
		GraphPID:      88,
		GraphBoltPort: 17687,
		GraphHTTPPort: 17474,
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	healthChecks := 0
	graphStopGraphHealthy = func(record eshulocal.OwnerRecord) bool {
		healthChecks++
		return healthChecks == 1
	}
	var signaledPID int
	graphSignalProcess = func(pid int, signal os.Signal) error {
		signaledPID = pid
		return nil
	}
	stopRecordedCalled := false
	graphStopRecordedGraph = func(record eshulocal.OwnerRecord) error {
		stopRecordedCalled = true
		return nil
	}
	graphStopPollInterval = time.Millisecond

	err := graphStopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("graphStopForLayout() error = %v, want nil", err)
	}
	if signaledPID != record.PID {
		t.Fatalf("signaledPID = %d, want owner pid %d", signaledPID, record.PID)
	}
	if stopRecordedCalled {
		t.Fatal("graphStopForLayout() stopped recorded graph directly while owner was live")
	}
}

func TestGraphStopStopsRecordedGraphWhenOwnerIsDead(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalGraphHealthy := graphStopGraphHealthy
	originalStopRecordedGraph := graphStopRecordedGraph
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphStopGraphHealthy = originalGraphHealthy
		graphStopRecordedGraph = originalStopRecordedGraph
	})

	record := eshulocal.OwnerRecord{
		PID:           42,
		WorkspaceID:   "workspace-id",
		Profile:       string(query.ProfileLocalAuthoritative),
		GraphBackend:  string(query.GraphBackendNornicDB),
		GraphPID:      88,
		GraphBoltPort: 17687,
		GraphHTTPPort: 17474,
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphProcessAlive = func(pid int) bool {
		return false
	}
	var stoppedPID int
	graphStopGraphHealthy = func(record eshulocal.OwnerRecord) bool {
		return stoppedPID == 0
	}
	graphStopRecordedGraph = func(record eshulocal.OwnerRecord) error {
		stoppedPID = record.GraphPID
		return nil
	}

	err := graphStopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("graphStopForLayout() error = %v, want nil", err)
	}
	if stoppedPID != record.GraphPID {
		t.Fatalf("stoppedPID = %d, want graph pid %d", stoppedPID, record.GraphPID)
	}
}

func TestGraphStopSignalsLightweightOwnerWhenAlive(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalSignalProcess := graphSignalProcess
	originalStopPollInterval := graphStopPollInterval
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphSignalProcess = originalSignalProcess
		graphStopPollInterval = originalStopPollInterval
	})

	record := eshulocal.OwnerRecord{
		PID:     99,
		Profile: string(query.ProfileLocalLightweight),
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	alive := true
	graphProcessAlive = func(pid int) bool {
		return alive && pid == record.PID
	}
	var signaledPID int
	graphSignalProcess = func(pid int, signal os.Signal) error {
		signaledPID = pid
		alive = false
		return nil
	}
	graphStopPollInterval = time.Millisecond

	err := graphStopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("graphStopForLayout() error = %v, want nil", err)
	}
	if signaledPID != record.PID {
		t.Fatalf("signaledPID = %d, want owner pid %d", signaledPID, record.PID)
	}
}

func TestGraphStopReturnsNilWhenLightweightOwnerAlreadyDead(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalSignalProcess := graphSignalProcess
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphSignalProcess = originalSignalProcess
	})

	record := eshulocal.OwnerRecord{
		PID:     99,
		Profile: string(query.ProfileLocalLightweight),
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphProcessAlive = func(pid int) bool { return false }
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatal("graphSignalProcess called for already-dead owner")
		return nil
	}

	err := graphStopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("graphStopForLayout() error = %v, want nil", err)
	}
}

func TestRunGraphUpgradeRequiresStoppedOwner(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalInstall := graphInstallNornicDB
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphInstallNornicDB = originalInstall
	})

	workspaceRoot := t.TempDir()
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.Layout{
			WorkspaceRoot:   workspaceRoot,
			WorkspaceID:     "workspace-id",
			OwnerRecordPath: "/workspace/owner.json",
		}, nil
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{
			PID:           42,
			Profile:       string(query.ProfileLocalAuthoritative),
			GraphBackend:  string(query.GraphBackendNornicDB),
			GraphPID:      88,
			GraphBoltPort: 17687,
			GraphHTTPPort: 17474,
		}, nil
	}
	graphProcessAlive = func(pid int) bool {
		return pid == 42
	}
	graphInstallNornicDB = func(opts installNornicDBOptions) (installNornicDBResult, error) {
		t.Fatal("graphInstallNornicDB called while owner was live")
		return installNornicDBResult{}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("from", "/tmp/nornicdb-headless", "")
	cmd.Flags().String("sha256", "", "")

	err := runGraphUpgrade(cmd, nil)
	if err == nil {
		t.Fatal("runGraphUpgrade() error = nil, want live-owner error")
	}
	if !strings.Contains(err.Error(), "eshu graph stop") {
		t.Fatalf("runGraphUpgrade() error = %q, want stop guidance", err.Error())
	}
}

func TestRunGraphUpgradeInstallsWithForceWhenStopped(t *testing.T) {
	originalGetwd := graphGetwd
	originalBuildLayout := graphBuildLayout
	originalReadOwnerRecord := graphReadOwnerRecord
	originalInstall := graphInstallNornicDB
	t.Cleanup(func() {
		graphGetwd = originalGetwd
		graphBuildLayout = originalBuildLayout
		graphReadOwnerRecord = originalReadOwnerRecord
		graphInstallNornicDB = originalInstall
	})

	workspaceRoot := t.TempDir()
	graphGetwd = func() (string, error) {
		return workspaceRoot, nil
	}
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.Layout{
			WorkspaceRoot:   workspaceRoot,
			WorkspaceID:     "workspace-id",
			OwnerRecordPath: "/workspace/owner.json",
		}, nil
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{}, os.ErrNotExist
	}
	var gotOptions installNornicDBOptions
	graphInstallNornicDB = func(opts installNornicDBOptions) (installNornicDBResult, error) {
		gotOptions = opts
		return installNornicDBResult{
			Installed:  true,
			BinaryPath: "/eshu/bin/nornicdb-headless",
			Version:    "v1.0.43",
		}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("from", "/tmp/nornicdb-headless", "")
	cmd.Flags().String("sha256", "abc123", "")

	output := captureStdout(t, func() {
		if err := runGraphUpgrade(cmd, nil); err != nil {
			t.Fatalf("runGraphUpgrade() error = %v, want nil", err)
		}
	})
	if !gotOptions.Force {
		t.Fatal("Force = false, want true for upgrade")
	}
	if gotOptions.From != "/tmp/nornicdb-headless" || gotOptions.SHA256 != "abc123" {
		t.Fatalf("upgrade options = %+v, want source/checksum flags", gotOptions)
	}
	if !strings.Contains(output, `"version": "v1.0.43"`) {
		t.Fatalf("runGraphUpgrade() output = %q, want JSON result", output)
	}
}

func TestGraphLifecycleNotWiredReturnsActionableError(t *testing.T) {
	err := graphLifecycleNotWired("eshu graph start")
	if err == nil {
		t.Fatal("graphLifecycleNotWired() error = nil, want non-nil")
	}
	if err.Error() != "eshu graph start not wired yet" {
		t.Fatalf("graphLifecycleNotWired() error = %q, want %q", err.Error(), "eshu graph start not wired yet")
	}
}
