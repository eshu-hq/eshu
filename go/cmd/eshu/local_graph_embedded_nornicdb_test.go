//go:build nolocalllm

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	norniccypher "github.com/orneryd/nornicdb/pkg/cypher"
	"github.com/orneryd/nornicdb/pkg/nornicdb"
)

func TestEmbeddedLocalNornicDBRuntimeRedirectsStandardLogger(t *testing.T) {
	original := log.Writer()
	t.Cleanup(func() {
		log.SetOutput(original)
	})

	var previous bytes.Buffer
	log.SetOutput(&previous)

	var embeddedLogs bytes.Buffer
	restore := redirectEmbeddedNornicDBStandardLogger(&embeddedLogs)
	log.Print("embedded startup line")
	restore()
	log.Print("owner line")

	if got := embeddedLogs.String(); !strings.Contains(got, "embedded startup line") {
		t.Fatalf("embedded log output = %q, want startup line", got)
	}
	if got := embeddedLogs.String(); strings.Contains(got, "owner line") {
		t.Fatalf("embedded log output = %q, want owner line restored away from embedded writer", got)
	}
	if got := previous.String(); !strings.Contains(got, "owner line") {
		t.Fatalf("previous log output = %q, want owner line after restore", got)
	}
	if got := previous.String(); strings.Contains(got, "embedded startup line") {
		t.Fatalf("previous log output = %q, want embedded line routed away", got)
	}
}

func TestEmbeddedLocalNornicDBRuntimeRedirectsStartupProcessOutput(t *testing.T) {
	var embeddedLogs bytes.Buffer

	restore, err := redirectEmbeddedNornicDBStartupProcessOutput(&embeddedLogs)
	if err != nil {
		t.Fatalf("redirectEmbeddedNornicDBStartupProcessOutput() error = %v, want nil", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, "embedded stdout startup line")
	_, _ = fmt.Fprintln(os.Stderr, "embedded stderr startup line")
	if err := restore(); err != nil {
		t.Fatalf("restore redirected process output: %v", err)
	}

	got := embeddedLogs.String()
	for _, want := range []string{"embedded stdout startup line", "embedded stderr startup line"} {
		if !strings.Contains(got, want) {
			t.Fatalf("embedded log output = %q, want %q", got, want)
		}
	}
}

func TestEmbeddedLocalNornicDBRuntimeLogsEffectiveSettings(t *testing.T) {
	previousParallel := norniccypher.GetParallelConfig()
	t.Cleanup(func() {
		norniccypher.SetParallelConfig(previousParallel)
	})

	norniccypher.SetParallelConfig(norniccypher.ParallelConfig{
		Enabled:      true,
		MaxWorkers:   3,
		MinBatchSize: 250,
	})
	config := nornicdb.DefaultConfig()

	var logs bytes.Buffer
	writeEmbeddedNornicDBRuntimeSettings(&logs, config)

	for _, want := range []string{
		"embedded nornicdb runtime settings",
		"parallel_enabled=true",
		"parallel_workers=3",
		"parallel_min_batch_size=250",
		"memory_limit=unlimited",
		"gc_percent=100",
		"object_pool_enabled=true",
		"query_cache_enabled=true",
		"query_cache_size=1000",
		"query_cache_ttl=5m0s",
	} {
		if got := logs.String(); !strings.Contains(got, want) {
			t.Fatalf("settings log = %q, want %q", got, want)
		}
	}
}

func TestEmbeddedLocalNornicDBRuntimeStartsHTTPAndBolt(t *testing.T) {
	boltPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve bolt port: %v", err)
	}
	httpPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve http port: %v", err)
	}

	credentials := localGraphCredentials{Username: "admin", Password: "embedded-secret"}
	runtime, err := startEmbeddedNornicDBRuntime(t.TempDir(), localNornicDBBindAddress, boltPort, httpPort, credentials, io.Discard)
	if err != nil {
		t.Fatalf("startEmbeddedNornicDBRuntime() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), localGraphShutdownTimeout)
		defer cancel()
		if err := runtime.stop(ctx); err != nil {
			t.Fatalf("stop embedded runtime: %v", err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if graphHTTPHealthy(localNornicDBBindAddress, httpPort, localGraphHealthTimeout) &&
			graphBoltHealthy(localNornicDBBindAddress, boltPort, localGraphHealthTimeout) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("embedded NornicDB runtime did not become healthy")
}

func TestEmbeddedLocalNornicDBRuntimeRequiresBoltCredentials(t *testing.T) {
	boltPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve bolt port: %v", err)
	}
	httpPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve http port: %v", err)
	}

	credentials := localGraphCredentials{Username: "admin", Password: "embedded-secret"}
	runtime, err := startEmbeddedNornicDBRuntime(t.TempDir(), localNornicDBBindAddress, boltPort, httpPort, credentials, io.Discard)
	if err != nil {
		t.Fatalf("startEmbeddedNornicDBRuntime() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), localGraphShutdownTimeout)
		defer cancel()
		if err := runtime.stop(ctx); err != nil {
			t.Fatalf("stop embedded runtime: %v", err)
		}
	})

	assertEmbeddedBoltNoAuthRejected(t, boltPort)
	assertEmbeddedBoltBasicAuthAccepted(t, boltPort, credentials)
}

func TestEmbeddedLocalNornicDBRuntimeAllowsAdminWritesToDefaultDatabase(t *testing.T) {
	boltPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve bolt port: %v", err)
	}
	httpPort, err := reserveLocalGraphPort()
	if err != nil {
		t.Fatalf("reserve http port: %v", err)
	}

	credentials := localGraphCredentials{Username: "admin", Password: "embedded-secret"}
	runtime, err := startEmbeddedNornicDBRuntime(t.TempDir(), localNornicDBBindAddress, boltPort, httpPort, credentials, io.Discard)
	if err != nil {
		t.Fatalf("startEmbeddedNornicDBRuntime() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), localGraphShutdownTimeout)
		defer cancel()
		if err := runtime.stop(ctx); err != nil {
			t.Fatalf("stop embedded runtime: %v", err)
		}
	})

	waitForEmbeddedNornicDBRuntimeHealthy(t, boltPort, httpPort)
	assertEmbeddedBoltDefaultDatabaseWriteAccepted(t, boltPort, credentials)
}

func waitForEmbeddedNornicDBRuntimeHealthy(t *testing.T, boltPort int, httpPort int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if graphHTTPHealthy(localNornicDBBindAddress, httpPort, localGraphHealthTimeout) &&
			graphBoltHealthy(localNornicDBBindAddress, boltPort, localGraphHealthTimeout) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("embedded NornicDB runtime did not become healthy")
}

func assertEmbeddedBoltNoAuthRejected(t *testing.T, boltPort int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(
		embeddedBoltURI(boltPort),
		neo4jdriver.NoAuth(),
	)
	if err != nil {
		t.Fatalf("new no-auth driver: %v", err)
	}
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})
	if err := driver.VerifyConnectivity(ctx); err == nil {
		t.Fatal("VerifyConnectivity() error = nil, want no-auth rejection")
	}
}

func assertEmbeddedBoltBasicAuthAccepted(t *testing.T, boltPort int, credentials localGraphCredentials) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(
		embeddedBoltURI(boltPort),
		neo4jdriver.BasicAuth(credentials.Username, credentials.Password, ""),
	)
	if err != nil {
		t.Fatalf("new basic-auth driver: %v", err)
	}
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("VerifyConnectivity() error = %v, want authenticated Bolt connection", err)
	}
}

func assertEmbeddedBoltDefaultDatabaseWriteAccepted(t *testing.T, boltPort int, credentials localGraphCredentials) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(
		embeddedBoltURI(boltPort),
		neo4jdriver.BasicAuth(credentials.Username, credentials.Password, ""),
	)
	if err != nil {
		t.Fatalf("new basic-auth driver: %v", err)
	}
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, "MERGE (p:EshuEmbeddedAccessProbe {id: $id}) RETURN p.id", map[string]any{
		"id": "default-database-write",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want write access to %q", err, localNornicDBDefaultDatabase)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("Consume() error = %v, want write access to %q", err, localNornicDBDefaultDatabase)
	}
}

func embeddedBoltURI(boltPort int) string {
	return "bolt://" + localNornicDBBindAddress + ":" + fmt.Sprint(boltPort)
}
