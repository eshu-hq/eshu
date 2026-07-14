// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const repoDependencyMidCommitProbeRows = 50000

// TestLiveRepoDependencyMidCommitDropQuiescesBeforeTakeover proves the pinned
// backend reaches one atomic terminal outcome after a complete Bolt COMMIT
// request is forwarded and both sides of that connection are immediately
// dropped. The probe waits the production graph-quiescence budget, performs a
// simulated new-owner replacement, then watches through the safety margin for
// a stale late commit.
func TestLiveRepoDependencyMidCommitDropQuiescesBeforeTakeover(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPO_MID_COMMIT_QUIESCENCE_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_REPO_MID_COMMIT_QUIESCENCE_PROVE_LIVE=1 to run the mid-COMMIT quiescence proof")
	}
	backendURI := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if backendURI == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	backendDriver, err := neo4jdriver.NewDriverWithContext(backendURI, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open direct graph driver: %v", err)
	}
	t.Cleanup(func() { _ = backendDriver.Close(context.Background()) })

	proxy := newBoltCommitDispatchDropProxy(t, backendURI)
	defer proxy.Close()
	proxyDriver, err := neo4jdriver.NewDriverWithContext(proxy.URI(), neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open proxied graph driver: %v", err)
	}
	defer func() { _ = proxyDriver.Close(context.Background()) }()

	probe := fmt.Sprintf("repo-mid-commit-%d", time.Now().UnixNano())
	takeoverProbe := probe + "-new-owner"
	directRunner := neo4jSessionRunner{Driver: backendDriver}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, identity := range []string{probe, takeoverProbe} {
			if cleanupErr := runRepoDependencyGroupProbe(
				cleanupCtx,
				directRunner,
				`MATCH (n:RepoDependencyGroupCancelProbe {probe: $probe}) DETACH DELETE n`,
				map[string]any{"probe": identity},
			); cleanupErr != nil {
				t.Errorf("clean mid-COMMIT probe %q: %v", identity, cleanupErr)
			}
		}
	})

	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Minute)
	writeErr := (neo4jSessionRunner{Driver: proxyDriver, TxTimeout: 2 * time.Minute}).RunCypherGroup(
		callCtx,
		[]sourcecypher.Statement{
			{
				Cypher: `UNWIND range(1, $rows) AS i
CREATE (:RepoDependencyGroupCancelProbe {probe: $probe, ordinal: i})`,
				Parameters: map[string]any{"probe": probe, "rows": repoDependencyMidCommitProbeRows},
			},
			{
				Cypher: `MATCH (n:RepoDependencyGroupCancelProbe {probe: $probe})
SET n.group_finished = true`,
				Parameters: map[string]any{"probe": probe},
			},
		},
	)
	callCancel()
	if writeErr == nil {
		t.Fatal("grouped write succeeded after the COMMIT connection was dropped")
	}
	proxy.WaitForCommitDispatch(t, ctx)

	const graphQuiescenceBudget = 2 * time.Minute
	const takeoverSafetyMargin = 30 * time.Second
	checkpoints := []time.Duration{5 * time.Second, 30 * time.Second, graphQuiescenceBudget}
	started := time.Now()
	terminalCount := int64(-1)
	for _, checkpoint := range checkpoints {
		waitUntil := started.Add(checkpoint)
		if delay := time.Until(waitUntil); delay > 0 {
			time.Sleep(delay)
		}
		count := countRepoDependencyGroupProbe(t, ctx, backendDriver, probe)
		if count != 0 && count != repoDependencyMidCommitProbeRows {
			t.Fatalf("mid-COMMIT outcome at %s contains %d rows, want atomic 0 or %d", checkpoint, count, repoDependencyMidCommitProbeRows)
		}
		if terminalCount < 0 {
			terminalCount = count
		} else if count != terminalCount {
			t.Fatalf("mid-COMMIT outcome changed from %d to %d rows by %s", terminalCount, count, checkpoint)
		}
		t.Logf("mid-COMMIT terminal count at %s: %d", checkpoint, count)
	}

	if err := directRunner.RunCypher(ctx,
		`MATCH (n:RepoDependencyGroupCancelProbe {probe: $probe}) DETACH DELETE n`,
		map[string]any{"probe": probe}); err != nil {
		t.Fatalf("simulate new-owner retract: %v", err)
	}
	if err := directRunner.RunCypher(ctx,
		`CREATE (:RepoDependencyGroupCancelProbe {probe: $probe, owner: 'new'})`,
		map[string]any{"probe": takeoverProbe}); err != nil {
		t.Fatalf("simulate new-owner write: %v", err)
	}

	time.Sleep(takeoverSafetyMargin)
	if count := countRepoDependencyGroupProbe(t, ctx, backendDriver, probe); count != 0 {
		t.Fatalf("old transaction reappeared after takeover with %d rows", count)
	}
	if count := countRepoDependencyGroupProbe(t, ctx, backendDriver, takeoverProbe); count != 1 {
		t.Fatalf("new-owner marker count = %d, want 1", count)
	}
}

type boltCommitDispatchDropProxy struct {
	listener     net.Listener
	backendAddr  string
	dispatched   chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	dispatchOnce sync.Once
}

func newBoltCommitDispatchDropProxy(t *testing.T, backendURI string) *boltCommitDispatchDropProxy {
	t.Helper()
	parsed, err := url.Parse(backendURI)
	if err != nil {
		t.Fatalf("parse ESHU_NEO4J_URI: %v", err)
	}
	if parsed.Scheme != "bolt" || parsed.Host == "" {
		t.Fatalf("ESHU_NEO4J_URI = %q, want a direct bolt:// URI", backendURI)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for Bolt mid-COMMIT proxy: %v", err)
	}
	proxy := &boltCommitDispatchDropProxy{
		listener:    listener,
		backendAddr: parsed.Host,
		dispatched:  make(chan struct{}),
		done:        make(chan struct{}),
	}
	go proxy.serve()
	return proxy
}

func (p *boltCommitDispatchDropProxy) URI() string {
	return "bolt://" + p.listener.Addr().String()
}

func (p *boltCommitDispatchDropProxy) WaitForCommitDispatch(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-p.dispatched:
	case <-ctx.Done():
		t.Fatalf("wait for forwarded COMMIT request: %v", ctx.Err())
	}
}

func (p *boltCommitDispatchDropProxy) Close() {
	p.closeOnce.Do(func() {
		_ = p.listener.Close()
		<-p.done
	})
}

func (p *boltCommitDispatchDropProxy) serve() {
	defer close(p.done)
	client, err := p.listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	backend, err := net.DialTimeout("tcp", p.backendAddr, 5*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = backend.Close() }()
	if err := relayBoltHandshake(client, backend); err != nil {
		return
	}

	errCh := make(chan error, 2)
	go func() { errCh <- p.forwardClientMessages(client, backend) }()
	go func() { errCh <- p.forwardServerMessages(backend, client) }()
	<-errCh
	_ = client.Close()
	_ = backend.Close()
}

func (p *boltCommitDispatchDropProxy) forwardClientMessages(client net.Conn, backend net.Conn) error {
	for {
		frame, payload, err := readBoltMessage(client)
		if err != nil {
			return err
		}
		if err := writeAll(backend, frame); err != nil {
			return err
		}
		if boltMessageTag(payload) == boltCommitMessageTag {
			p.dispatchOnce.Do(func() { close(p.dispatched) })
			return io.EOF
		}
	}
}

func (p *boltCommitDispatchDropProxy) forwardServerMessages(backend net.Conn, client net.Conn) error {
	for {
		frame, _, err := readBoltMessage(backend)
		if err != nil {
			return err
		}
		if err := writeAll(client, frame); err != nil {
			return err
		}
	}
}
