// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestTrackedBootstrapDialForceClosesCanceledPGXCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	queryStarted := make(chan struct{})
	serverDone := make(chan struct{})
	var serverWorkers sync.WaitGroup
	go func() {
		defer close(serverDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			serverWorkers.Add(1)
			go func() {
				defer serverWorkers.Done()
				serveCleanupShim(conn, queryStarted)
			}()
		}
	}()

	tracked := &bootstrapDialTracker{}
	config, err := pgx.ParseConfig("postgres://fixture:fixture@127.0.0.1:5432/eshu?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	baseDial := (&net.Dialer{}).DialContext
	config.DialFunc = tracked.wrap(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return baseDial(ctx, network, listener.Addr().String())
	})
	conn, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect cleanup shim: %v", err)
	}
	queryCtx, cancelQuery := context.WithCancel(context.Background())
	queryDone := make(chan error, 1)
	go func() {
		var result int
		queryDone <- conn.QueryRow(queryCtx, "SELECT 1").Scan(&result)
	}()
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("shim did not receive query")
	}
	cancelQuery()
	select {
	case <-queryDone:
	case <-time.After(time.Second):
		t.Fatal("canceled query did not return")
	}
	if !conn.PgConn().IsClosed() {
		t.Fatal("pgx did not mark the canceled bootstrap connection closed")
	}
	if err := tracked.closeAll(); err != nil {
		t.Errorf("force-close tracked sockets: %v", err)
	}
	select {
	case <-conn.PgConn().CleanupDone():
	case <-time.After(time.Second):
		t.Fatal("tracked socket force-close did not finish pgx async cleanup")
	}
	if active := tracked.activeSockets(); active != 0 {
		t.Fatalf("live tracked sockets after cleanup = %d, want 0", active)
	}
	_ = listener.Close()
	select {
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() {
			<-serverDone
			serverWorkers.Wait()
			close(done)
		}()
		return done
	}():
	case <-time.After(time.Second):
		t.Fatal("shim server connection goroutines did not exit")
	}
}

func serveCleanupShim(conn net.Conn, queryStarted chan<- struct{}) {
	defer conn.Close()
	backend := pgproto3.NewBackend(conn, conn)
	startup, err := backend.ReceiveStartupMessage()
	if err != nil {
		return
	}
	if _, ok := startup.(*pgproto3.CancelRequest); ok {
		return
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	for _, parameter := range []pgproto3.ParameterStatus{
		{Name: "server_version", Value: "16.0"},
		{Name: "client_encoding", Value: "UTF8"},
		{Name: "DateStyle", Value: "ISO, MDY"},
		{Name: "IntervalStyle", Value: "postgres"},
		{Name: "standard_conforming_strings", Value: "on"},
		{Name: "TimeZone", Value: "UTC"},
	} {
		backend.Send(&parameter)
	}
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{1, 2, 3, 4}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return
	}
	for {
		message, err := backend.Receive()
		if err != nil {
			return
		}
		switch message.(type) {
		case *pgproto3.Query, *pgproto3.Parse:
			select {
			case queryStarted <- struct{}{}:
			default:
			}
			_, _ = backend.Receive()
			return
		}
	}
}
