// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

// hookShim is a DSN-free Postgres backend that answers only what RunFence.Hook
// asks: the shared advisory lock and the "does the run lock session still hold
// (5318,1)" query. held is what that query reports, so a test trips the fence
// the way a lost lock does by flipping it. It follows the pgproto3 shim pattern
// of internal/runtime/postgres.
type hookShim struct {
	held        atomic.Bool
	sharedLocks atomic.Int32
	heldQueries atomic.Int32
}

// connect starts the shim and returns a pgx connection to it. The connection
// uses the simple protocol so each statement is one Query message.
func (s *hookShim) connect(t *testing.T) *pgx.Conn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				s.serve(conn)
			}()
		}
	}()
	config, err := pgx.ParseConfig("postgres://fixture:fixture@127.0.0.1:5432/eshu?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	baseDial := (&net.Dialer{}).DialContext
	config.DialFunc = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return baseDial(ctx, network, listener.Addr().String())
	}
	conn, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect hook shim: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
		_ = listener.Close()
		<-acceptDone
		workers.Wait()
	})
	return conn
}

func (s *hookShim) serve(conn net.Conn) {
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
		query, ok := message.(*pgproto3.Query)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(query.String, "pg_advisory_lock_shared"):
			s.sharedLocks.Add(1)
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		case strings.Contains(query.String, "pg_locks"):
			s.heldQueries.Add(1)
			answer := []byte("f")
			if s.held.Load() {
				answer = []byte("t")
			}
			backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
				{Name: []byte("exists"), DataTypeOID: 16, DataTypeSize: 1, TypeModifier: -1},
			}})
			backend.Send(&pgproto3.DataRow{Values: [][]byte{answer}})
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		default:
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 0")})
		}
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if err := backend.Flush(); err != nil {
			return
		}
	}
}
