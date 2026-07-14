// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	boltCommitMessageTag  = byte(0x12)
	boltSuccessMessageTag = byte(0x70)
)

func TestLiveRepoDependencyGroupedCommitResponseLossIsExactlyReplayable(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPO_COMMIT_LOSS_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_REPO_COMMIT_LOSS_PROVE_LIVE=1 to run the COMMIT response-loss proof")
	}
	backendURI := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if backendURI == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backendDriver, err := neo4jdriver.NewDriverWithContext(backendURI, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open direct graph driver: %v", err)
	}
	defer func() { _ = backendDriver.Close(context.Background()) }()

	proxy := newBoltCommitLossProxy(t, backendURI)
	defer proxy.Close()
	proxyDriver, err := neo4jdriver.NewDriverWithContext(proxy.URI(), neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open proxied graph driver: %v", err)
	}
	defer func() { _ = proxyDriver.Close(context.Background()) }()

	unique := fmt.Sprintf("repo-commit-loss-%d", time.Now().UnixNano())
	sourceID := unique + "-source"
	targetID := unique + "-target"
	evidenceSource := "proof/repo-commit-loss"
	resolvedID := unique + "-resolved"
	directRunner := neo4jSessionRunner{Driver: backendDriver}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupRepoCommitLossIdentities(t, cleanupCtx, directRunner, sourceID, targetID)
	}()

	row := reducer.SharedProjectionIntentRow{
		IntentID:     unique + "-intent",
		RepositoryID: sourceID,
		GenerationID: unique + "-generation",
		Payload: map[string]any{
			"repo_id":              sourceID,
			"target_repo_id":       targetID,
			"relationship_type":    "DEPENDS_ON",
			"resolved_id":          resolvedID,
			"evidence_count":       2,
			"evidence_kinds":       []string{"manifest", "runtime"},
			"resolution_source":    "repo-commit-loss-proof",
			"confidence":           0.99,
			"rationale":            "prove ambiguous grouped COMMIT replay",
			"source_tool":          "ifa",
			"generation_id":        unique + "-generation",
			"acceptance_unit_id":   unique + "-acceptance",
			"source_repository_id": sourceID,
		},
	}

	proxiedWriter := sourcecypher.NewEdgeWriter(
		newReducerNeo4jExecutor(neo4jSessionRunner{Driver: proxyDriver}, nil),
		0,
	)
	writeErr := proxiedWriter.WriteEdges(ctx, reducer.DomainRepoDependency, []reducer.SharedProjectionIntentRow{row}, evidenceSource)
	if writeErr == nil {
		t.Fatal("grouped write error = nil after COMMIT SUCCESS was dropped")
	}
	if !strings.Contains(writeErr.Error(), "Connection lost during commit") {
		t.Fatalf("grouped write error = %v, want commit-ambiguous classification", writeErr)
	}
	proxy.WaitForDroppedCommit(t, ctx)
	committed := readRepoCommitLossEdge(t, ctx, directRunner, sourceID, targetID, evidenceSource, resolvedID)

	directWriter := sourcecypher.NewEdgeWriter(
		newReducerNeo4jExecutor(directRunner, nil),
		0,
	)
	if err := directWriter.WriteEdges(ctx, reducer.DomainRepoDependency, []reducer.SharedProjectionIntentRow{row}, evidenceSource); err != nil {
		t.Fatalf("idempotent replay after ambiguous COMMIT: %v", err)
	}
	replayed := readRepoCommitLossEdge(t, ctx, directRunner, sourceID, targetID, evidenceSource, resolvedID)
	if !reflect.DeepEqual(replayed, committed) {
		t.Fatalf("replayed edge = %#v, want exact committed edge %#v", replayed, committed)
	}
}

func cleanupRepoCommitLossIdentities(
	t *testing.T,
	ctx context.Context,
	runner neo4jSessionRunner,
	ids ...string,
) {
	t.Helper()
	for _, id := range ids {
		if err := runner.RunCypher(ctx,
			`MATCH (n:Repository {id: $id}) DETACH DELETE n`,
			map[string]any{"id": id}); err != nil {
			t.Errorf("delete repository proof identity %q: %v", id, err)
			continue
		}
		rows, err := runner.Run(ctx,
			`MATCH (n:Repository {id: $id}) RETURN n.id AS id`,
			map[string]any{"id": id})
		if err != nil {
			t.Errorf("verify repository proof identity %q cleanup: %v", id, err)
			continue
		}
		if len(rows) != 0 {
			t.Errorf("repository proof identity %q remains after cleanup", id)
		}
	}
}

func readRepoCommitLossEdge(
	t *testing.T,
	ctx context.Context,
	runner neo4jSessionRunner,
	sourceID string,
	targetID string,
	evidenceSource string,
	resolvedID string,
) map[string]any {
	t.Helper()
	rows, err := runner.Run(ctx, `MATCH (:Repository {id: $source_id})-[rel:DEPENDS_ON]->(:Repository {id: $target_id})
RETURN rel.evidence_source AS evidence_source,
       rel.resolved_id AS resolved_id,
	   rel.relationship_type AS relationship_type,
	   rel.generation_id AS generation_id,
	   rel.evidence_count AS evidence_count,
	   rel.evidence_kinds AS evidence_kinds,
	   rel.resolution_source AS resolution_source,
	   rel.confidence AS confidence,
	   rel.rationale AS rationale,
	   rel.source_tool AS source_tool`, map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	})
	if err != nil {
		t.Fatalf("read committed repository dependency: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("repository dependency count = %d, want exactly 1", len(rows))
	}
	if got := rows[0]["evidence_source"]; got != evidenceSource {
		t.Fatalf("evidence_source = %v, want %q", got, evidenceSource)
	}
	if got := rows[0]["resolved_id"]; got != resolvedID {
		t.Fatalf("resolved_id = %v, want %q", got, resolvedID)
	}
	if got := rows[0]["relationship_type"]; got != "DEPENDS_ON" {
		t.Fatalf("relationship_type = %v, want DEPENDS_ON", got)
	}
	return rows[0]
}

type boltCommitLossProxy struct {
	listener      net.Listener
	backendAddr   string
	commitPending atomic.Bool
	dropped       chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
	dropOnce      sync.Once
}

func newBoltCommitLossProxy(t *testing.T, backendURI string) *boltCommitLossProxy {
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
		t.Fatalf("listen for Bolt proxy: %v", err)
	}
	proxy := &boltCommitLossProxy{
		listener:    listener,
		backendAddr: parsed.Host,
		dropped:     make(chan struct{}),
		done:        make(chan struct{}),
	}
	go proxy.serve()
	return proxy
}

func (p *boltCommitLossProxy) URI() string {
	return "bolt://" + p.listener.Addr().String()
}

func (p *boltCommitLossProxy) WaitForDroppedCommit(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-p.dropped:
	case <-ctx.Done():
		t.Fatalf("wait for dropped COMMIT SUCCESS: %v", ctx.Err())
	}
}

func (p *boltCommitLossProxy) Close() {
	p.closeOnce.Do(func() {
		_ = p.listener.Close()
		<-p.done
	})
}

func (p *boltCommitLossProxy) serve() {
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

func relayBoltHandshake(client net.Conn, backend net.Conn) error {
	clientHandshake := make([]byte, 20)
	if _, err := io.ReadFull(client, clientHandshake); err != nil {
		return err
	}
	if err := writeAll(backend, clientHandshake); err != nil {
		return err
	}
	serverVersion := make([]byte, 4)
	if _, err := io.ReadFull(backend, serverVersion); err != nil {
		return err
	}
	return writeAll(client, serverVersion)
}

func (p *boltCommitLossProxy) forwardClientMessages(client net.Conn, backend net.Conn) error {
	for {
		frame, payload, err := readBoltMessage(client)
		if err != nil {
			return err
		}
		if boltMessageTag(payload) == boltCommitMessageTag {
			p.commitPending.Store(true)
		}
		if err := writeAll(backend, frame); err != nil {
			return err
		}
	}
}

func (p *boltCommitLossProxy) forwardServerMessages(backend net.Conn, client net.Conn) error {
	for {
		frame, payload, err := readBoltMessage(backend)
		if err != nil {
			return err
		}
		if p.commitPending.Load() && boltMessageTag(payload) == boltSuccessMessageTag {
			p.dropOnce.Do(func() { close(p.dropped) })
			return io.EOF
		}
		if err := writeAll(client, frame); err != nil {
			return err
		}
	}
}

func readBoltMessage(conn net.Conn) ([]byte, []byte, error) {
	var frame bytes.Buffer
	var payload bytes.Buffer
	for {
		var sizeBytes [2]byte
		if _, err := io.ReadFull(conn, sizeBytes[:]); err != nil {
			return nil, nil, err
		}
		frame.Write(sizeBytes[:])
		size := int(binary.BigEndian.Uint16(sizeBytes[:]))
		if size == 0 {
			return frame.Bytes(), payload.Bytes(), nil
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(conn, chunk); err != nil {
			return nil, nil, err
		}
		frame.Write(chunk)
		payload.Write(chunk)
	}
}

func boltMessageTag(payload []byte) byte {
	if len(payload) < 2 || payload[0]&0xf0 != 0xb0 {
		return 0
	}
	return payload[1]
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		written, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}
