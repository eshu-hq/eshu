// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type metadataCause struct{ user, database, host string }

type metadataConnector struct{ cause error }

func (c metadataConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.cause }
func (metadataConnector) Driver() driver.Driver                          { return terminalErrorDriver{} }

func (e metadataCause) Error() string {
	return "user=" + e.user + " dbname=" + e.database + " host=" + e.host
}

func TestPrivateFailureFormatsAndUnwrapsTypedCause(t *testing.T) {
	cause := metadataCause{user: "private_user", database: "private_db", host: "private_host"}
	joined := privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, cause))
	if !errors.Is(joined, ErrReaderUnavailable) {
		t.Fatal("reader classification lost")
	}
	for _, err := range []error{joined, privateFailure(failureReaderBorrow, cause)} {
		var typed metadataCause
		if !errors.As(err, &typed) || typed != cause {
			t.Fatalf("private typed cause lost: %+v", typed)
		}
		for _, format := range []string{"%v", "%s", "%+v", "%#v"} {
			got := fmt.Sprintf(format, err)
			if strings.Contains(got, "private_") {
				t.Errorf("format %s exposed backend metadata: %s", format, got)
			}
		}
		if got := fmt.Errorf("route: %w", err).Error(); strings.Contains(got, "private_") {
			t.Errorf("wrapped public error exposed backend metadata: %s", got)
		}
	}
}

func TestWriterCheckpointReaderBorrowAndPingKeepMetadataPrivate(t *testing.T) {
	cause := metadataCause{user: "private_user", database: "private_db", host: "private_host"}
	failingPool := sql.OpenDB(metadataConnector{cause: cause})
	defer failingPool.Close()
	access := &Access{writer: failingPool, reader: failingPool, replayTimeout: time.Second}
	check := func(label string, err error, marker error) {
		t.Helper()
		if !errors.Is(err, marker) {
			t.Fatalf("%s lost classification: %v", label, err)
		}
		var typed metadataCause
		if !errors.As(err, &typed) || typed != cause {
			t.Fatalf("%s lost private typed cause", label)
		}
		if strings.Contains(err.Error(), "private_") {
			t.Errorf("%s exposed backend metadata: %v", label, err)
		}
	}
	_, err := access.ContextWithCheckpoint(t.Context())
	check("writer checkpoint", err, ErrWriterUnavailable)
	checkpointCtx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access})
	_, err = access.borrowFresh(checkpointCtx)
	check("reader borrow", err, ErrReaderUnavailable)
	if err := access.Ping(t.Context()); err == nil || strings.Contains(err.Error(), "private_") {
		t.Fatalf("writer ping leaked or succeeded: %v", err)
	}
}

func TestReaderTerminalErrorsKeepPrivateDriverCause(t *testing.T) {
	cause := errors.New("postgres user=private_user dbname=private_db host=private_host password=private_password")
	check := func(label string, err error) {
		t.Helper()
		if !errors.Is(err, cause) {
			t.Fatalf("%s lost driver cause", label)
		}
		for _, format := range []string{"%v", "%s", "%+v", "%#v", "%w"} {
			var public string
			if format == "%w" {
				public = fmt.Errorf("status: %w", err).Error()
			} else {
				public = fmt.Sprintf(format, err)
			}
			if strings.Contains(public, "private_") {
				t.Errorf("%s %s leaked backend metadata: %s", label, format, public)
			}
		}
	}
	check("row close", (&fencedRow{rows: &failingCloseRows{next: true, closeErr: cause}}).Scan(new(int)))
	check("row cursor", (&fencedRow{rows: &failingCloseRows{cursorErr: cause}}).Scan(new(int)))
	tx := &readTransaction{err: cause}
	tx.once.Do(func() {})
	terminal := tx.finish(false)
	if !errors.Is(terminal, sql.ErrTxDone) {
		t.Fatalf("finished transaction lost ErrTxDone: %v", terminal)
	}
	check("transaction terminal", terminal)
}

func TestReaderDomainErrorsRemainClassified(t *testing.T) {
	row := &fencedRow{rows: &failingCloseRows{}}
	if err := row.Scan(new(int)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty row: %v", err)
	}
	if err := replayContextError(context.DeadlineExceeded); !errors.Is(err, ErrReaderStale) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale deadline: %v", err)
	}
}
