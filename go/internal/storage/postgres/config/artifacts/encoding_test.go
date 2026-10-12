// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// encodingQueryer answers SHOW server_encoding with a fixed value or error.
type encodingQueryer struct {
	value string
	err   error
	query string
}

func (q *encodingQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.query = query
	if q.err != nil {
		return nil, q.err
	}
	return &stringRows{values: []string{q.value}}, nil
}

// stringRows is a one-column db.Rows over fixed strings.
type stringRows struct {
	values []string
	next   int
}

func (r *stringRows) Next() bool { r.next++; return r.next <= len(r.values) }
func (r *stringRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.values[r.next-1]
	return nil
}
func (r *stringRows) Err() error   { return nil }
func (r *stringRows) Close() error { return nil }

// logRecords returns the JSON log records written to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		out = append(out, record)
	}
	return out
}

func TestCheckEncodingAcceptsUTF8(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	queryer := &encodingQueryer{value: "UTF8"}
	got := CheckEncoding(context.Background(), queryer, slog.New(slog.NewJSONHandler(&buf, nil)))

	if !got.Supported || got.ServerEncoding != "UTF8" || got.Reason != "" {
		t.Fatalf("CheckEncoding(UTF8) = %+v, want supported with no reason", got)
	}
	if queryer.query != showServerEncodingSQL {
		t.Fatalf("query = %q, want %q", queryer.query, showServerEncodingSQL)
	}
	records := logRecords(t, &buf)
	if len(records) != 1 || records[0]["level"] != "INFO" ||
		records[0]["event_name"] != "postgres.server_encoding" || records[0]["server_encoding"] != "UTF8" {
		t.Fatalf("log records = %v, want one INFO postgres.server_encoding with server_encoding=UTF8", records)
	}
}

func TestCheckEncodingRefusesANonUTF8Database(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	got := CheckEncoding(context.Background(), &encodingQueryer{value: "SQL_ASCII"},
		slog.New(slog.NewJSONHandler(&buf, nil)))

	if got.Supported || got.ServerEncoding != "SQL_ASCII" || got.Reason != ReasonEncodingUnsupported {
		t.Fatalf("CheckEncoding(SQL_ASCII) = %+v, want unsupported with reason %q", got, ReasonEncodingUnsupported)
	}
	records := logRecords(t, &buf)
	if len(records) != 1 || records[0]["level"] != "ERROR" ||
		records[0]["event_name"] != "postgres.server_encoding" ||
		records[0]["server_encoding"] != "SQL_ASCII" ||
		records[0]["config_artifacts_status"] != StatusNotInstalled ||
		records[0]["reason"] != ReasonEncodingUnsupported {
		t.Fatalf("log records = %v, want one ERROR postgres.server_encoding naming not_installed and the reason", records)
	}
}

// TestCheckEncodingFailsClosedOnAQueryError keeps the read model off when the
// check cannot run: a derive must not write digests it cannot vouch for.
func TestCheckEncodingFailsClosedOnAQueryError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	got := CheckEncoding(context.Background(), &encodingQueryer{err: errors.New("connection reset")},
		slog.New(slog.NewJSONHandler(&buf, nil)))

	if got.Supported || got.Reason != ReasonEncodingCheckFailed {
		t.Fatalf("CheckEncoding(error) = %+v, want unsupported with reason %q", got, ReasonEncodingCheckFailed)
	}
	records := logRecords(t, &buf)
	if len(records) != 1 || records[0]["level"] != "ERROR" || records[0]["reason"] != ReasonEncodingCheckFailed ||
		!strings.Contains(records[0]["error"].(string), "connection reset") {
		t.Fatalf("log records = %v, want one ERROR naming the check failure and its error", records)
	}
}

func TestCheckEncodingToleratesANilLogger(t *testing.T) {
	t.Parallel()

	if got := CheckEncoding(context.Background(), &encodingQueryer{value: "LATIN1"}, nil); got.Supported {
		t.Fatalf("CheckEncoding(LATIN1) = %+v, want unsupported", got)
	}
}
