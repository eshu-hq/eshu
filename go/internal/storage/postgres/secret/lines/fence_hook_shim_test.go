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
	// closed counts sessions the client ended, by a Terminate message or by
	// dropping the socket, so a test can tell a closed connection from a leaked one.
	closed atomic.Int32
	// terminated counts the graceful Terminate messages clients sent; a socket
	// dropped without one counts only in closed.
	terminated atomic.Int32
	// failSessionSQL makes the shim answer the writer-session SET with an error,
	// as a server that rejects the session setting would.
	failSessionSQL atomic.Bool
	// accepted holds every server-side socket so cleanup can end sessions a
	// client leaked; without it a leaked session would hang the test cleanup.
	acceptedMu sync.Mutex
	accepted   []net.Conn
}

// connect starts the shim and returns a pgx connection to it. The connection
// uses the simple protocol so each statement is one Query message.
func (s *hookShim) connect(t *testing.T) *pgx.Conn {
	t.Helper()
	config := s.connConfig(t)
	conn, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect hook shim: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	})
	return conn
}

// connConfig starts the shim listener and returns a pgx config that dials it,
// for tests that open the connection through database/sql instead of pgx.
func (s *hookShim) connConfig(t *testing.T) *pgx.ConnConfig {
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
			s.acceptedMu.Lock()
			s.accepted = append(s.accepted, conn)
			s.acceptedMu.Unlock()
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
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		s.acceptedMu.Lock()
		for _, accepted := range s.accepted {
			_ = accepted.Close()
		}
		s.acceptedMu.Unlock()
		workers.Wait()
	})
	return config
}

func (s *hookShim) serve(conn net.Conn) {
	defer conn.Close()
	backend := pgproto3.NewBackend(conn, conn)
	started := false
	defer func() {
		if started {
			s.closed.Add(1)
		}
	}()
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
	started = true
	for {
		message, err := backend.Receive()
		if err != nil {
			return
		}
		if _, terminate := message.(*pgproto3.Terminate); terminate {
			s.terminated.Add(1)
			return
		}
		query, ok := message.(*pgproto3.Query)
		if !ok {
			continue
		}
		switch {
		case s.failSessionSQL.Load() && strings.Contains(query.String, "eshu.infra_inventory_writer"):
			backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42501", Message: "permission denied to set parameter"})
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
