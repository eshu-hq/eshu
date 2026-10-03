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
	"github.com/jackc/pgx/v5/stdlib"
)

func TestFleetBeginHonorsSetupDeadlineBeforeRequestDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	beginSeen := make(chan struct{}, 1)
	serverDone := make(chan struct{})
	var workers sync.WaitGroup
	go func() {
		defer close(serverDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				serveCleanupShim(conn, beginSeen)
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
	pool := stdlib.OpenDB(*config)
	defer pool.Close()
	ownerCtx, cancelOwner := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancelOwner()
	setupCtx, cancelSetup := context.WithTimeout(ownerCtx, 100*time.Millisecond)
	defer cancelSetup()
	conn, err := pool.Conn(ownerCtx)
	if err != nil {
		t.Fatalf("connect shim: %v", err)
	}
	defer conn.Close()
	started := time.Now()
	_, err = beginReadTransactionOwned(setupCtx, ownerCtx, &readerConnection{Conn: conn}, &Access{})
	elapsed := time.Since(started)
	select {
	case <-beginSeen:
	default:
		t.Fatal("shim did not receive BEGIN")
	}
	if err == nil {
		t.Fatal("stalled BEGIN unexpectedly succeeded")
	}
	_ = tracked.closeAll()
	_ = listener.Close()
	<-serverDone
	workers.Wait()
	if elapsed > 250*time.Millisecond {
		t.Fatalf("stalled BEGIN took %s past 100ms setup budget; request deadline was 600ms", elapsed)
	}
	if ownerCtx.Err() != nil {
		t.Fatalf("setup failure consumed request deadline: %v", ownerCtx.Err())
	}
}
