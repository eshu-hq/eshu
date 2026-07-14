// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveRepoDependencyGraphMutexProveTheory tests whether a unique-key
// reservation held in an uncommitted graph transaction can serialize an
// external multi-transaction repository replacement. It is a throwaway
// prove-theory shim, not production behavior.
func TestLiveRepoDependencyGraphMutexProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPO_MUTEX_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_REPO_MUTEX_PROVE_LIVE=1 to run the graph-mutex shim")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := runLiveGuardWriteLedger(ctx, driver, "", `
CREATE CONSTRAINT repo_dependency_mutex_probe_unique IF NOT EXISTS
FOR (n:RepoDependencyMutexProbe) REQUIRE n.lock_key IS UNIQUE`, nil); err != nil {
		t.Fatalf("create mutex constraint: %v", err)
	}

	for trial := 0; trial < 10; trial++ {
		key := fmt.Sprintf("repo-mutex-%d-%d", time.Now().UnixNano(), trial)
		lockA := acquireRepoDependencyGraphMutex(t, ctx, driver, key)

		enteredB := make(chan *repoDependencyGraphMutex, 1)
		errB := make(chan error, 1)
		go func() {
			lock, acquireErr := tryAcquireRepoDependencyGraphMutex(ctx, driver, key)
			if acquireErr != nil {
				errB <- acquireErr
				return
			}
			enteredB <- lock
		}()

		select {
		case lockB := <-enteredB:
			lockB.release(context.Background())
			lockA.release(context.Background())
			t.Fatalf("trial %d: same-repository writer entered while A held the graph mutex", trial)
		case acquireErr := <-errB:
			lockA.release(context.Background())
			t.Fatalf("trial %d: B failed instead of waiting/retrying: %v", trial, acquireErr)
		case <-time.After(100 * time.Millisecond):
		}

		lockOther := acquireRepoDependencyGraphMutex(t, ctx, driver, key+"-other")
		lockOther.release(context.Background())
		lockA.release(context.Background())

		select {
		case lockB := <-enteredB:
			lockB.release(context.Background())
		case acquireErr := <-errB:
			t.Fatalf("trial %d: B did not acquire after A released: %v", trial, acquireErr)
		case <-ctx.Done():
			t.Fatalf("trial %d: B did not acquire after A released", trial)
		}
	}
}

// TestLiveNornicCanceledWriteRollsBackProveTheory verifies the timeout premise
// required by any lease-grace design: after the client context is canceled, a
// slow write must not commit later on the server.
func TestLiveNornicCanceledWriteRollsBackProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPO_MUTEX_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_REPO_MUTEX_PROVE_LIVE=1 to run the canceled-write shim")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	probe := fmt.Sprintf("timeout-probe-%d", time.Now().UnixNano())
	defer func() {
		_ = runLiveGuardWriteLedger(context.Background(), driver, "",
			`MATCH (n:RepoDependencyTimeoutProbe {probe: $probe}) DETACH DELETE n`,
			map[string]any{"probe": probe})
	}()
	if err := runLiveGuardWriteLedger(ctx, driver, "", `
UNWIND range(1, 3) AS i
CREATE (:RepoDependencyTimeoutProbe {probe: $probe, ordinal: -i})`, map[string]any{"probe": probe}); err != nil {
		t.Fatalf("seed timeout negative control: %v", err)
	}
	if count := readRepoDependencyTimeoutProbeCount(t, ctx, driver, probe); count != 3 {
		t.Fatalf("uncanceled negative control wrote %d nodes, want 3", count)
	}
	if err := runLiveGuardWriteLedger(ctx, driver, "",
		`MATCH (n:RepoDependencyTimeoutProbe {probe: $probe}) DETACH DELETE n`,
		map[string]any{"probe": probe}); err != nil {
		t.Fatalf("clear timeout negative control: %v", err)
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite})
	writeCtx, writeCancel := context.WithTimeout(ctx, time.Millisecond)
	result, runErr := session.Run(writeCtx, `
UNWIND range(1, 50000) AS i
CREATE (:RepoDependencyTimeoutProbe {probe: $probe, ordinal: i})`, map[string]any{"probe": probe})
	if runErr == nil {
		_, runErr = result.Consume(writeCtx)
	}
	writeCancel()
	_ = session.Close(context.Background())
	if runErr == nil {
		t.Fatal("slow write unexpectedly completed inside the 1ms timeout")
	}

	time.Sleep(500 * time.Millisecond)
	if count := readRepoDependencyTimeoutProbeCount(t, ctx, driver, probe); count != 0 {
		t.Fatalf("canceled write committed %d nodes after the client timeout; lease grace cannot fence stale graph commits", count)
	}
}

func readRepoDependencyTimeoutProbeCount(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	probe string,
) int64 {
	t.Helper()
	readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
	defer func() { _ = readSession.Close(context.Background()) }()
	readResult, err := readSession.Run(ctx,
		`MATCH (n:RepoDependencyTimeoutProbe {probe: $probe}) RETURN count(n) AS count`,
		map[string]any{"probe": probe})
	if err != nil {
		t.Fatalf("read timeout probe: %v", err)
	}
	record, err := readResult.Single(ctx)
	if err != nil {
		t.Fatalf("read timeout probe row: %v", err)
	}
	value, _ := record.Get("count")
	count, _ := value.(int64)
	return count
}

type repoDependencyGraphMutex struct {
	session neo4jdriver.SessionWithContext
	tx      neo4jdriver.ExplicitTransaction
}

func acquireRepoDependencyGraphMutex(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	key string,
) *repoDependencyGraphMutex {
	t.Helper()
	lock, err := tryAcquireRepoDependencyGraphMutex(ctx, driver, key)
	if err != nil {
		t.Fatalf("acquire graph mutex %q: %v", key, err)
	}
	return lock
}

func tryAcquireRepoDependencyGraphMutex(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	key string,
) (*repoDependencyGraphMutex, error) {
	for attempt := 0; attempt < 100; attempt++ {
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite})
		tx, err := session.BeginTransaction(ctx)
		if err != nil {
			_ = session.Close(ctx)
			return nil, err
		}
		result, err := tx.Run(ctx, `CREATE (:RepoDependencyMutexProbe {lock_key: $key})`, map[string]any{"key": key})
		if err == nil {
			_, err = result.Consume(ctx)
		}
		if err == nil {
			return &repoDependencyGraphMutex{session: session, tx: tx}, nil
		}
		_ = tx.Rollback(ctx)
		_ = session.Close(ctx)
		if !strings.Contains(err.Error(), "Constraint") &&
			!strings.Contains(err.Error(), "Outdated") &&
			!strings.Contains(err.Error(), "already exists") {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("graph mutex %q remained contended", key)
}

func (m *repoDependencyGraphMutex) release(ctx context.Context) {
	if m == nil {
		return
	}
	_ = m.tx.Rollback(ctx)
	_ = m.session.Close(ctx)
}
