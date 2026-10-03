// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestReaderQualificationRunsCandidatesConcurrentlyAndKeepsOrdinal(t *testing.T) {
	inventory := []ReaderMember{{ID: "blackhole", Host: "unused", Port: 5432}, {ID: "healthy", Host: "unused", Port: 5433}}
	started := make(chan int, len(inventory))
	releaseBlackhole := make(chan struct{})
	type result struct {
		members []physicalReaderMember
		err     error
	}
	finished := make(chan result, 1)
	go func() {
		members, err := qualifyReaderCandidates(context.Background(), inventory, 0, func(ctx context.Context, ordinal int, member ReaderMember) (physicalReaderMember, error) {
			started <- ordinal
			if ordinal == 0 {
				select {
				case <-releaseBlackhole:
					return physicalReaderMember{}, fmt.Errorf("dial %s: %w", member.ID, syscall.ECONNREFUSED)
				case <-ctx.Done():
					return physicalReaderMember{}, ctx.Err()
				}
			}
			return physicalReaderMember{id: member.ID, ordinal: ordinal}, nil
		})
		finished <- result{members: members, err: err}
	}()

	seen := map[int]bool{}
	for range len(inventory) {
		select {
		case ordinal := <-started:
			seen[ordinal] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatal("candidate qualification was sequential; second member did not start beside the blackholed first")
		}
	}
	close(releaseBlackhole)
	select {
	case got := <-finished:
		if got.err != nil {
			t.Fatalf("qualification error: %v", got.err)
		}
		if len(got.members) != 1 || got.members[0].id != "healthy" || got.members[0].ordinal != 1 {
			t.Fatalf("qualified members lost their inventory ordinal: %+v", got.members)
		}
	case <-time.After(time.Second):
		t.Fatal("qualification workers did not join")
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("candidate starts = %v", seen)
	}
}

func TestReaderOpenKeepsHealthyMemberWhenFirstMemberBlackholes(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned primary and physical standby not configured")
	}
	endpoint, err := parsePhysicalEndpoint(reader)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var sockets sync.WaitGroup
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			sockets.Add(1)
			go func() {
				defer sockets.Done()
				<-stop
				_ = conn.Close()
			}()
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = listener.Close()
		<-done
		sockets.Wait()
	})
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReadMembers = []ReaderMember{
		{ID: "blackhole", Host: "127.0.0.1", Port: uint16(listener.Addr().(*net.TCPAddr).Port)},
		{ID: "healthy", Host: endpoint.Host, Port: endpoint.Port},
	}
	cfg.PingTimeout = 3 * time.Second
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("healthy physical reader was hidden by blackhole: %v", err)
	}
	t.Cleanup(func() { _ = access.Close() })
	if len(access.readerMembers) != 1 || access.readerMembers[0].id != "healthy" {
		t.Fatalf("qualified readers = %+v, want only healthy member", access.readerMembers)
	}
}

func TestReaderQualificationDelayedFatalCannotBeHiddenByHealthyPeer(t *testing.T) {
	inventory := []ReaderMember{{ID: "auth-failure", Host: "unused", Port: 5432}, {ID: "healthy", Host: "unused", Port: 5433}}
	started := make(chan int, len(inventory))
	releaseFatal := make(chan struct{})
	healthyReturned := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := qualifyReaderCandidates(context.Background(), inventory, 0, func(ctx context.Context, ordinal int, _ ReaderMember) (physicalReaderMember, error) {
			started <- ordinal
			if ordinal == 0 {
				select {
				case <-releaseFatal:
					return physicalReaderMember{}, &pgconn.PgError{Code: "28P01"}
				case <-ctx.Done():
					return physicalReaderMember{}, ctx.Err()
				}
			}
			close(healthyReturned)
			return physicalReaderMember{id: "healthy", ordinal: ordinal}, nil
		})
		finished <- err
	}()
	for range len(inventory) {
		select {
		case <-started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("qualification candidates did not start concurrently")
		}
	}
	select {
	case <-healthyReturned:
	case <-time.After(time.Second):
		t.Fatal("healthy peer did not finish while auth qualification was delayed")
	}
	close(releaseFatal)
	select {
	case err := <-finished:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "28P01" {
			t.Fatalf("delayed auth failure was hidden: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("qualification workers did not join after a fatal result")
	}
}

func TestReaderQualificationKeepsSlowValidMemberThroughSharedStage(t *testing.T) {
	const stage = 300 * time.Millisecond
	inventory := []ReaderMember{{ID: "slow-valid"}, {ID: "blackhole"}}
	started := time.Now()
	members, err := qualifyReaderCandidates(context.Background(), inventory, stage, func(ctx context.Context, ordinal int, _ ReaderMember) (physicalReaderMember, error) {
		if ordinal == 0 {
			select {
			case <-time.After(200 * time.Millisecond):
				return physicalReaderMember{id: "slow-valid", ordinal: ordinal}, nil
			case <-ctx.Done():
				return physicalReaderMember{}, ctx.Err()
			}
		}
		<-ctx.Done()
		return physicalReaderMember{}, ctx.Err()
	})
	if err != nil || len(members) != 1 || members[0].id != "slow-valid" {
		t.Fatalf("stage qualification = %+v, %v; want slow valid member", members, err)
	}
	if elapsed := time.Since(started); elapsed > 2*stage {
		t.Fatalf("shared stage took %s, want at most %s", elapsed, 2*stage)
	}
}

func TestReaderQualificationOverallDeadlineRejectsCompletedPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	inventory := []ReaderMember{{ID: "healthy"}, {ID: "blocked"}}
	members, err := qualifyReaderCandidates(ctx, inventory, time.Second, func(ctx context.Context, ordinal int, _ ReaderMember) (physicalReaderMember, error) {
		if ordinal == 0 {
			return physicalReaderMember{id: "healthy", ordinal: ordinal}, nil
		}
		<-ctx.Done()
		return physicalReaderMember{}, ctx.Err()
	})
	if len(members) != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired overall deadline = %+v, %v; want no members and deadline", members, err)
	}
}

func TestReaderQualificationStageTimeoutDoesNotHideDelayedAuth(t *testing.T) {
	inventory := []ReaderMember{{ID: "auth"}, {ID: "healthy"}}
	members, err := qualifyReaderCandidates(context.Background(), inventory, 30*time.Millisecond, func(ctx context.Context, ordinal int, _ ReaderMember) (physicalReaderMember, error) {
		if ordinal == 0 {
			<-ctx.Done()
			return physicalReaderMember{}, errors.Join(ctx.Err(), &pgconn.PgError{Code: "28P01"})
		}
		return physicalReaderMember{id: "healthy", ordinal: ordinal}, nil
	})
	var pgErr *pgconn.PgError
	if len(members) != 0 || !errors.As(err, &pgErr) || pgErr.Code != "28P01" {
		t.Fatalf("delayed authentication result = %+v, %v; want fatal authentication", members, err)
	}
}

func TestReaderQualificationPreservesIdentityBesideCleanupFailure(t *testing.T) {
	writer := physicalIdentity{systemID: "1", database: "eshu"}
	decoded := physicalIdentity{recovery: "true", readOnly: "on", defaultReadOnly: "on", systemID: "2", database: "eshu", incarnation: "reader"}
	cleanupErr := context.DeadlineExceeded
	err := readerMemberQualificationError(nil, decoded, writer, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}, cleanupErr)
	if !errors.Is(err, ErrWrongTopology) || !errors.Is(err, cleanupErr) {
		t.Fatalf("identity and cleanup result = %v; want both causes", err)
	}
	if transientReaderMemberFailure(context.Background(), err) {
		t.Fatalf("decoded identity contradiction became transient: %v", err)
	}
}

func TestReaderQualificationFailureClasses(t *testing.T) {
	dnsFailure := &net.DNSError{Err: "no such host", Name: "reader.invalid", IsNotFound: true}
	cases := []struct {
		name      string
		err       error
		parentErr error
		transient bool
	}{
		{name: "dns", err: dnsFailure, transient: true},
		{name: "refused", err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), transient: true},
		{name: "reset", err: fmt.Errorf("read: %w", syscall.ECONNRESET), transient: true},
		{name: "eof", err: fmt.Errorf("connect failed: %w", io.EOF), transient: true},
		{name: "sqlstate connection exception", err: &pgconn.PgError{Code: "08006"}, transient: true},
		{name: "sqlstate not connected", err: &pgconn.PgError{Code: "08003"}, transient: true},
		{name: "sqlstate unable to connect", err: &pgconn.PgError{Code: "08001"}, transient: true},
		{name: "other class 08 is fatal", err: &pgconn.PgError{Code: "08004"}},
		{name: "sqlstate shutdown", err: &pgconn.PgError{Code: "57P03"}, transient: true},
		{name: "sqlstate capacity", err: &pgconn.PgError{Code: "53300"}, transient: true},
		{name: "authentication", err: &pgconn.PgError{Code: "28P01"}},
		{name: "authentication joined with deadline", err: errors.Join(context.DeadlineExceeded, &pgconn.PgError{Code: "28P01"})},
		{name: "authentication after transient PostgreSQL failure", err: errors.Join(&pgconn.PgError{Code: "08006"}, &pgconn.PgError{Code: "28P01"})},
		{name: "transient with unavailable sentinel", err: errors.Join(ErrReaderUnavailable, syscall.ECONNRESET), transient: true},
		{name: "bare unavailable sentinel", err: ErrReaderUnavailable},
		{name: "unknown after transient transport failure", err: errors.Join(syscall.ECONNRESET, errors.New("unexpected setup failure"))},
		{name: "wrong topology after transient transport failure", err: errors.Join(syscall.ECONNRESET, ErrWrongTopology)},
		{name: "permission", err: &pgconn.PgError{Code: "42501"}},
		{name: "missing database", err: &pgconn.PgError{Code: "3D000"}},
		{name: "tls hostname", err: x509.HostnameError{Host: "reader.invalid"}},
		{name: "tls protocol", err: tls.RecordHeaderError{Msg: "bad TLS record"}},
		{name: "generic net op is fatal", err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("unknown network error")}},
		{name: "malformed metadata", err: errors.New("invalid PostgreSQL identity metadata")},
		{name: "unknown", err: errors.New("unexpected setup failure")},
		{name: "phase deadline while parent lives", err: context.DeadlineExceeded, transient: true},
		{name: "overall deadline", err: context.DeadlineExceeded, parentErr: context.DeadlineExceeded},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if test.parentErr != nil {
				cancel()
			}
			defer cancel()
			if got := transientReaderMemberFailure(ctx, test.err); got != test.transient {
				t.Fatalf("transient classification=%t, want %t", got, test.transient)
			}
		})
	}
}

func TestReaderMemberIdentityContradictionRequiresDecodedIdentity(t *testing.T) {
	writer := physicalIdentity{recovery: "false", readOnly: "off", defaultReadOnly: "off", systemID: "1", database: "eshu", incarnation: "writer"}
	addresses := []net.IP{net.ParseIP("10.0.0.2")}
	decoded := physicalIdentity{recovery: "true", readOnly: "on", defaultReadOnly: "on", systemID: "2", database: "eshu", incarnation: "reader"}
	if err := validateReaderMemberIdentity(decoded, writer, "10.0.0.2", addresses); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("decoded system-ID contradiction = %v, want ErrWrongTopology", err)
	}
	if transientReaderMemberFailure(context.Background(), errors.New("invalid PostgreSQL identity metadata")) {
		t.Fatal("malformed identity decode was treated as transient")
	}
}

func TestCanceledBootstrapJoinsAsyncCleanupBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cleanupDone := make(chan struct{})
	forceCloseCalled := make(chan struct{})
	releaseCleanup := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- awaitPGXCleanup(ctx, cleanupDone, func() error {
			close(forceCloseCalled)
			go func() {
				<-releaseCleanup
				close(cleanupDone)
			}()
			return nil
		})
	}()
	select {
	case <-forceCloseCalled:
	case <-time.After(time.Second):
		t.Fatal("canceled bootstrap did not force-close its PostgreSQL socket")
	}
	select {
	case err := <-finished:
		t.Fatalf("bootstrap returned before async cleanup joined: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(releaseCleanup)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cleanup error = %v, want canceled context", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bootstrap worker did not join async PostgreSQL cleanup")
	}
}
