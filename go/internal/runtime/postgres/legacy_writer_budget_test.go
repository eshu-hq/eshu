// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func TestLegacyWriterBootstrapKeepsFullPingBudget(t *testing.T) {
	writerDSN := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	if writerDSN == "" {
		t.Skip("owned PostgreSQL primary not configured")
	}
	upstream, err := parsePhysicalEndpoint(writerDSN)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var workers sync.WaitGroup
	var clientsMu sync.Mutex
	var clients []net.Conn
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			client, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			clientsMu.Lock()
			clients = append(clients, client)
			clientsMu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				server, dialErr := net.Dial("tcp", net.JoinHostPort(upstream.Host, fmt.Sprint(upstream.Port)))
				if dialErr != nil {
					return
				}
				defer server.Close()
				workers.Add(1)
				go func() {
					defer workers.Done()
					_, _ = io.Copy(server, client)
				}()
				first := make([]byte, 4096)
				n, readErr := server.Read(first)
				if readErr != nil {
					return
				}
				once.Do(func() { time.Sleep(500 * time.Millisecond) })
				if _, writeErr := client.Write(first[:n]); writeErr != nil {
					return
				}
				_, _ = io.Copy(client, server)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		clientsMu.Lock()
		for _, client := range clients {
			_ = client.Close()
		}
		clientsMu.Unlock()
		<-serverDone
		workers.Wait()
	})
	proxyPort := listener.Addr().(*net.TCPAddr).Port
	proxyDSN := fmt.Sprintf("host=127.0.0.1 port=%d user=postgres dbname=postgres sslmode=disable", proxyPort)
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN", "ESHU_POSTGRES_READ_DSN":
			return proxyDSN
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.PingTimeout = 1200 * time.Millisecond
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("healthy legacy writer behind 500ms bootstrap delay: %v", err)
	}
	if err := access.Close(); err != nil {
		t.Fatal(err)
	}
}
