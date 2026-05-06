//go:build nolocalllm

package main

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

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

	assertEmbeddedBoltDefaultDatabaseWriteAccepted(t, boltPort, credentials)
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
