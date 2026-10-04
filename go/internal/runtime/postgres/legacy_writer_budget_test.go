// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
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
	var servers []net.Conn
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
				clientsMu.Lock()
				servers = append(servers, server)
				clientsMu.Unlock()
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
		<-serverDone
		clientsMu.Lock()
		for _, client := range clients {
			_ = client.Close()
		}
		for _, server := range servers {
			_ = server.Close()
		}
		clientsMu.Unlock()
		workers.Wait()
	})
	proxyPort := listener.Addr().(*net.TCPAddr).Port
	proxyDSN, err := legacyWriterProxyDSN(writerDSN, proxyPort)
	if err != nil {
		t.Fatal(err)
	}
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

func TestLegacyWriterProxyDSNRetainsFixtureIdentity(t *testing.T) {
	for _, raw := range []string{
		"host=writer.internal port=5432 user=fixture_user dbname=fixture_db password=fixture_password sslmode=disable application_name=fixture_probe",
		"postgres://fixture_user:fixture_password@writer.internal:5432/fixture_db?sslmode=disable&application_name=fixture_probe",
	} {
		original, err := parsePhysicalEndpoint(raw)
		if err != nil {
			t.Fatal(err)
		}
		proxyDSN, err := legacyWriterProxyDSN(raw, 15432)
		if err != nil {
			t.Fatal(err)
		}
		proxied, err := parsePhysicalEndpoint(proxyDSN)
		if err != nil {
			t.Fatal(err)
		}
		if proxied.Host != "127.0.0.1" || proxied.Port != 15432 || proxied.User != original.User || proxied.Database != original.Database ||
			proxied.Password != original.Password || proxied.RuntimeParams["application_name"] != original.RuntimeParams["application_name"] {
			t.Fatalf("proxy changed fixture identity or options: host=%q port=%d user=%q database=%q", proxied.Host, proxied.Port, proxied.User, proxied.Database)
		}
	}
}

func legacyWriterProxyDSN(raw string, port int) (string, error) {
	if strings.HasPrefix(raw, "postgres://") || strings.HasPrefix(raw, "postgresql://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		parsed.Host = net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
		return parsed.String(), nil
	}
	return raw + fmt.Sprintf(" host=127.0.0.1 port=%d", port), nil
}
