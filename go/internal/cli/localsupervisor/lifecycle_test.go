// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"errors"
	"os"
	"testing"
	"time"

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

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if signaledPID != record.PID {
		t.Fatalf("signaledPID = %d, want owner pid %d", signaledPID, record.PID)
	}
	if stopRecordedCalled {
		t.Fatal("StopForLayout() stopped recorded graph directly while owner was live")
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

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if stoppedPID != record.GraphPID {
		t.Fatalf("stoppedPID = %d, want graph pid %d", stoppedPID, record.GraphPID)
	}
}

func TestGraphStopReclaimsDeadAuthoritativeOwnerRecord(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalAcquireOwnerLock := graphAcquireOwnerLock
	originalProcessAlive := graphProcessAlive
	originalGraphHealthy := graphStopGraphHealthy
	originalSignalProcess := graphSignalProcess
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphAcquireOwnerLock = originalAcquireOwnerLock
		graphProcessAlive = originalProcessAlive
		graphStopGraphHealthy = originalGraphHealthy
		graphSignalProcess = originalSignalProcess
	})

	ownerRecordPath := t.TempDir() + "/owner.json"
	if err := os.WriteFile(ownerRecordPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write owner record: %v", err)
	}
	record := eshulocal.OwnerRecord{
		PID:          42,
		WorkspaceID:  "workspace-id",
		Profile:      string(query.ProfileLocalAuthoritative),
		GraphBackend: string(query.GraphBackendNornicDB),
		GraphPID:     42,
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphAcquireOwnerLock = func(path string) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	graphProcessAlive = func(pid int) bool {
		return false
	}
	graphStopGraphHealthy = func(record eshulocal.OwnerRecord) bool {
		return false
	}
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatalf("graphSignalProcess called for already-dead owner pid %d", pid)
		return nil
	}

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: ownerRecordPath})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if _, err := os.Stat(ownerRecordPath); !os.IsNotExist(err) {
		t.Fatalf("owner record stat error = %v, want not exist", err)
	}
}

func TestGraphStopSignalsLightweightOwnerWhenAlive(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalSocketHealthy := graphOwnerSocketHealthy
	originalSignalProcess := graphSignalProcess
	originalStopPollInterval := graphStopPollInterval
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphOwnerSocketHealthy = originalSocketHealthy
		graphSignalProcess = originalSignalProcess
		graphStopPollInterval = originalStopPollInterval
	})

	record := eshulocal.OwnerRecord{
		PID:                99,
		Profile:            string(query.ProfileLocalLightweight),
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	alive := true
	graphProcessAlive = func(pid int) bool {
		return alive && pid == record.PID
	}
	graphOwnerSocketHealthy = func(path string) bool {
		return path == record.PostgresSocketPath
	}
	var signaledPID int
	graphSignalProcess = func(pid int, signal os.Signal) error {
		signaledPID = pid
		alive = false
		return nil
	}
	graphStopPollInterval = time.Millisecond

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: "/workspace/owner.json"})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if signaledPID != record.PID {
		t.Fatalf("signaledPID = %d, want owner pid %d", signaledPID, record.PID)
	}
}

func TestGraphStopCleansStaleLightweightRecordWithoutSignalingReusedPID(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalAcquireOwnerLock := graphAcquireOwnerLock
	originalProcessAlive := graphProcessAlive
	originalSocketHealthy := graphOwnerSocketHealthy
	originalSignalProcess := graphSignalProcess
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphAcquireOwnerLock = originalAcquireOwnerLock
		graphProcessAlive = originalProcessAlive
		graphOwnerSocketHealthy = originalSocketHealthy
		graphSignalProcess = originalSignalProcess
	})

	ownerRecordPath := t.TempDir() + "/owner.json"
	if err := os.WriteFile(ownerRecordPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write owner record: %v", err)
	}
	record := eshulocal.OwnerRecord{
		PID:                99,
		Profile:            string(query.ProfileLocalLightweight),
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphAcquireOwnerLock = func(path string) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	graphProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	graphOwnerSocketHealthy = func(path string) bool {
		return false
	}
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatalf("graphSignalProcess called for unverified pid %d", pid)
		return nil
	}

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: ownerRecordPath})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if _, err := os.Stat(ownerRecordPath); !os.IsNotExist(err) {
		t.Fatalf("owner record stat error = %v, want not exist", err)
	}
}

func TestGraphStopKeepsLightweightRecordWhenOwnerLockHeldAndSocketUnhealthy(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalAcquireOwnerLock := graphAcquireOwnerLock
	originalProcessAlive := graphProcessAlive
	originalSocketHealthy := graphOwnerSocketHealthy
	originalSignalProcess := graphSignalProcess
	originalStopPostgres := graphStopPostgres
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphAcquireOwnerLock = originalAcquireOwnerLock
		graphProcessAlive = originalProcessAlive
		graphOwnerSocketHealthy = originalSocketHealthy
		graphSignalProcess = originalSignalProcess
		graphStopPostgres = originalStopPostgres
	})

	ownerRecordPath := t.TempDir() + "/owner.json"
	if err := os.WriteFile(ownerRecordPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write owner record: %v", err)
	}
	record := eshulocal.OwnerRecord{
		PID:                99,
		Profile:            string(query.ProfileLocalLightweight),
		PostgresPID:        77,
		PostgresDataDir:    "/tmp/eshu/postgres",
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphAcquireOwnerLock = func(path string) (*eshulocal.OwnerLock, error) {
		return nil, eshulocal.ErrWorkspaceOwned
	}
	graphProcessAlive = func(pid int) bool {
		return pid == record.PID
	}
	graphOwnerSocketHealthy = func(path string) bool {
		return false
	}
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatalf("graphSignalProcess called for unverified pid %d", pid)
		return nil
	}
	graphStopPostgres = func(dataDir string) error {
		t.Fatalf("graphStopPostgres called while owner lock is held for %q", dataDir)
		return nil
	}

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: ownerRecordPath})
	if !errors.Is(err, eshulocal.ErrWorkspaceOwned) {
		t.Fatalf("StopForLayout() error = %v, want %v", err, eshulocal.ErrWorkspaceOwned)
	}
	if _, err := os.Stat(ownerRecordPath); err != nil {
		t.Fatalf("owner record stat error = %v, want record preserved", err)
	}
}

func TestGraphStopStopsStaleLightweightPostgresBeforeRemovingRecord(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalAcquireOwnerLock := graphAcquireOwnerLock
	originalProcessAlive := graphProcessAlive
	originalSocketHealthy := graphOwnerSocketHealthy
	originalSignalProcess := graphSignalProcess
	originalStopPostgres := graphStopPostgres
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphAcquireOwnerLock = originalAcquireOwnerLock
		graphProcessAlive = originalProcessAlive
		graphOwnerSocketHealthy = originalSocketHealthy
		graphSignalProcess = originalSignalProcess
		graphStopPostgres = originalStopPostgres
	})

	ownerRecordPath := t.TempDir() + "/owner.json"
	if err := os.WriteFile(ownerRecordPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write owner record: %v", err)
	}
	record := eshulocal.OwnerRecord{
		PID:                99,
		Profile:            string(query.ProfileLocalLightweight),
		PostgresPID:        77,
		PostgresDataDir:    "/tmp/eshu/postgres",
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphAcquireOwnerLock = func(path string) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	postgresAlive := true
	graphProcessAlive = func(pid int) bool {
		return pid == record.PostgresPID && postgresAlive
	}
	graphOwnerSocketHealthy = func(path string) bool {
		return false
	}
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatalf("graphSignalProcess called for stale owner pid %d", pid)
		return nil
	}
	stopCalls := 0
	graphStopPostgres = func(dataDir string) error {
		stopCalls++
		if dataDir != record.PostgresDataDir {
			t.Fatalf("graphStopPostgres() dataDir = %q, want %q", dataDir, record.PostgresDataDir)
		}
		postgresAlive = false
		return nil
	}

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: ownerRecordPath})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
	if stopCalls != 1 {
		t.Fatalf("graphStopPostgres() call count = %d, want 1", stopCalls)
	}
	if _, err := os.Stat(ownerRecordPath); !os.IsNotExist(err) {
		t.Fatalf("owner record stat error = %v, want not exist", err)
	}
}

func TestGraphStopReturnsNilWhenLightweightOwnerAlreadyDead(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalAcquireOwnerLock := graphAcquireOwnerLock
	originalProcessAlive := graphProcessAlive
	originalSignalProcess := graphSignalProcess
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphAcquireOwnerLock = originalAcquireOwnerLock
		graphProcessAlive = originalProcessAlive
		graphSignalProcess = originalSignalProcess
	})

	ownerRecordPath := t.TempDir() + "/owner.json"
	if err := os.WriteFile(ownerRecordPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write owner record: %v", err)
	}
	record := eshulocal.OwnerRecord{
		PID:     99,
		Profile: string(query.ProfileLocalLightweight),
	}
	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return record, nil
	}
	graphAcquireOwnerLock = func(path string) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	graphProcessAlive = func(pid int) bool { return false }
	graphSignalProcess = func(pid int, signal os.Signal) error {
		t.Fatal("graphSignalProcess called for already-dead owner")
		return nil
	}

	err := StopForLayout(eshulocal.Layout{OwnerRecordPath: ownerRecordPath})
	if err != nil {
		t.Fatalf("StopForLayout() error = %v, want nil", err)
	}
}
