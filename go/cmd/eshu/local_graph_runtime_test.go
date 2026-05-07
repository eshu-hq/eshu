package main

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestUseProcessLocalNornicDBDefaultsToEmbedded(t *testing.T) {
	got, err := useProcessLocalNornicDB(func(string) string { return "" }, true)
	if err != nil {
		t.Fatalf("useProcessLocalNornicDB() error = %v, want nil", err)
	}
	if got {
		t.Fatal("useProcessLocalNornicDB() = true, want embedded default")
	}
}

func TestUseProcessLocalNornicDBRequiresEmbeddedForDefaultMode(t *testing.T) {
	_, err := useProcessLocalNornicDB(func(string) string { return "" }, false)
	if err == nil {
		t.Fatal("useProcessLocalNornicDB() error = nil, want embedded build guidance")
	}
	if !strings.Contains(err.Error(), "embedded NornicDB is not available") {
		t.Fatalf("useProcessLocalNornicDB() error = %q, want embedded build guidance", err.Error())
	}
}

func TestUseProcessLocalNornicDBIgnoresBinaryWithoutProcessRuntime(t *testing.T) {
	got, err := useProcessLocalNornicDB(func(key string) string {
		if key == "ESHU_NORNICDB_BINARY" {
			return "/tmp/nornicdb-headless"
		}
		return ""
	}, true)
	if err != nil {
		t.Fatalf("useProcessLocalNornicDB() error = %v, want nil", err)
	}
	if got {
		t.Fatal("useProcessLocalNornicDB() = true, want embedded mode unless runtime is process")
	}
}

func TestUseProcessLocalNornicDBHonorsProcessRuntimeWithBinary(t *testing.T) {
	got, err := useProcessLocalNornicDB(func(key string) string {
		switch key {
		case localNornicDBRuntimeModeEnv:
			return "process"
		case "ESHU_NORNICDB_BINARY":
			return "/tmp/nornicdb-headless"
		default:
			return ""
		}
	}, true)
	if err != nil {
		t.Fatalf("useProcessLocalNornicDB() error = %v, want nil", err)
	}
	if !got {
		t.Fatal("useProcessLocalNornicDB() = false, want process mode for explicit runtime")
	}
}

func TestStartManagedLocalNornicDBDefaultsToEmbeddedRuntime(t *testing.T) {
	originalEmbedded := localGraphStartEmbedded
	originalProcess := localGraphStartProcess
	originalAvailable := localGraphEmbeddedAvailable
	t.Cleanup(func() {
		localGraphStartEmbedded = originalEmbedded
		localGraphStartProcess = originalProcess
		localGraphEmbeddedAvailable = originalAvailable
	})
	t.Setenv(localNornicDBRuntimeModeEnv, "")
	t.Setenv("ESHU_NORNICDB_BINARY", "")
	localGraphEmbeddedAvailable = func() bool { return true }

	embeddedCalled := false
	localGraphStartEmbedded = func(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
		embeddedCalled = true
		return &managedLocalGraph{Backend: query.GraphBackendNornicDB, PID: os.Getpid()}, nil
	}
	localGraphStartProcess = func(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
		t.Fatal("process runtime selected despite embedded default")
		return nil, nil
	}

	graph, err := startManagedLocalNornicDB(context.Background(), eshulocal.Layout{})
	if err != nil {
		t.Fatalf("startManagedLocalNornicDB() error = %v, want nil", err)
	}
	if graph == nil || !embeddedCalled {
		t.Fatal("startManagedLocalNornicDB() did not start embedded runtime")
	}
}

func TestStartManagedLocalNornicDBCanUseProcessRuntime(t *testing.T) {
	originalEmbedded := localGraphStartEmbedded
	originalProcess := localGraphStartProcess
	originalAvailable := localGraphEmbeddedAvailable
	t.Cleanup(func() {
		localGraphStartEmbedded = originalEmbedded
		localGraphStartProcess = originalProcess
		localGraphEmbeddedAvailable = originalAvailable
	})
	t.Setenv(localNornicDBRuntimeModeEnv, "process")
	t.Setenv("ESHU_NORNICDB_BINARY", "")
	localGraphEmbeddedAvailable = func() bool { return true }

	processCalled := false
	localGraphStartEmbedded = func(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
		t.Fatal("embedded runtime selected despite process override")
		return nil, nil
	}
	localGraphStartProcess = func(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
		processCalled = true
		return &managedLocalGraph{Backend: query.GraphBackendNornicDB, PID: 1234}, nil
	}

	graph, err := startManagedLocalNornicDB(context.Background(), eshulocal.Layout{})
	if err != nil {
		t.Fatalf("startManagedLocalNornicDB() error = %v, want nil", err)
	}
	if graph == nil || !processCalled {
		t.Fatal("startManagedLocalNornicDB() did not start process runtime")
	}
}

func TestUseProcessLocalNornicDBRejectsUndocumentedRuntimeAliases(t *testing.T) {
	for _, mode := range []string{"sidecar", "binary"} {
		t.Run(mode, func(t *testing.T) {
			_, err := useProcessLocalNornicDB(func(key string) string {
				if key == localNornicDBRuntimeModeEnv {
					return mode
				}
				return ""
			}, true)
			if err == nil {
				t.Fatal("useProcessLocalNornicDB() error = nil, want invalid runtime mode")
			}
			if !strings.Contains(err.Error(), "must be embedded or process") {
				t.Fatalf("useProcessLocalNornicDB() error = %q, want documented runtime values", err.Error())
			}
		})
	}
}

func TestGraphBoltHealthyReturnsFalseWhenServerAcceptsButIgnoresProtocol(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	if graphBoltHealthy(addr.IP.String(), addr.Port, time.Second) {
		t.Fatal("graphBoltHealthy() = true for server that closes without Bolt response, want false")
	}
}

func TestGraphBoltHealthyReturnsTrueWhenServerRespondsToHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var buf [20]byte
		_, _ = io.ReadFull(conn, buf[:])
		_, _ = conn.Write([]byte{0x00, 0x00, 0x00, 0x05}) // Bolt 5.0
	}()
	addr := ln.Addr().(*net.TCPAddr)
	if !graphBoltHealthy(addr.IP.String(), addr.Port, time.Second) {
		t.Fatal("graphBoltHealthy() = false for server that completes Bolt handshake, want true")
	}
}

func TestStopManagedLocalGraphUsesEmbeddedShutdown(t *testing.T) {
	shutdownCalled := false
	graph := &managedLocalGraph{
		logFile: io.NopCloser(strings.NewReader("")),
		shutdown: func(ctx context.Context) error {
			shutdownCalled = true
			return nil
		},
	}

	if err := stopManagedLocalGraph(graph, time.Second); err != nil {
		t.Fatalf("stopManagedLocalGraph() error = %v, want nil", err)
	}
	if !shutdownCalled {
		t.Fatal("stopManagedLocalGraph() did not call embedded shutdown")
	}
}
