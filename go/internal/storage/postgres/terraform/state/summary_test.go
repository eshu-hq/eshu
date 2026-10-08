// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statestore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

var summaryObservedAt = time.Date(2026, 10, 7, 9, 30, 15, 123456000, time.UTC)

func summarySerialRow(hash, serial string, observed sql.NullTime) []any {
	return []any{hash, "s3", "lineage-" + hash, serial, "terraform_state:state_snapshot:s3:" + hash + ":lineage-" + hash + ":serial:" + serial, observed}
}

func summaryWarningRow(hash, kind string, observed sql.NullTime) []any {
	return []any{hash, "s3", kind, "reason-x", "warning", "operator", "terraform_state", "handle-" + kind, "gen-1", observed}
}

func summaryFakeQueryer(serials, warnings [][]any) *fake.ExecQueryer {
	return &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: serials}, {Data: warnings}}}
}

func TestSummaryEntriesStoreWhatTheLiveReadReturns(t *testing.T) {
	t.Parallel()

	serials := [][]any{
		summarySerialRow("hash-a", "42", sql.NullTime{Time: summaryObservedAt, Valid: true}),
		summarySerialRow("hash-b", "7", sql.NullTime{}),
	}
	warnings := [][]any{
		summaryWarningRow("hash-a", "state_missing", sql.NullTime{Time: summaryObservedAt, Valid: true}),
		summaryWarningRow("hash-a", "sensitive_skip", sql.NullTime{Time: summaryObservedAt.Add(-time.Hour), Valid: true}),
		summaryWarningRow("hash-b", "state_missing", sql.NullTime{}),
	}
	live, err := ReadTerraformStateAdminEvidence(context.Background(), summaryFakeQueryer(serials, warnings), statuspkg.MaxTerraformStateRecentWarnings, summaryObservedAt)
	if err != nil {
		t.Fatalf("ReadTerraformStateAdminEvidence() error = %v", err)
	}

	entries, err := SummaryEntries(context.Background(), summaryFakeQueryer(serials, warnings))
	if err != nil {
		t.Fatalf("SummaryEntries() error = %v", err)
	}
	sections := []string{}
	for _, entry := range entries {
		sections = append(sections, entry.Section)
	}
	if want := []string{"last_serial", "last_serial", "recent_warning", "recent_warning", "recent_warning"}; !reflect.DeepEqual(sections, want) {
		t.Fatalf("sections = %v, want %v in live order", sections, want)
	}
	for i, entry := range entries {
		if wantOrdinal := int64(i + 1); i < 2 && entry.Ordinal != wantOrdinal {
			t.Fatalf("serial entry %d ordinal = %d, want %d", i, entry.Ordinal, wantOrdinal)
		}
	}

	decoded, err := DecodeSummaryEntries(entries)
	if err != nil {
		t.Fatalf("DecodeSummaryEntries() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, live) {
		t.Fatalf("decoded = %#v, want the live read %#v", decoded, live)
	}
	if !decoded.RecentWarnings[0].ObservedAt.Equal(summaryObservedAt) {
		t.Fatalf("observed_at = %v, want %v with its sub-second part", decoded.RecentWarnings[0].ObservedAt, summaryObservedAt)
	}
	if !decoded.LastSerials[1].ObservedAt.IsZero() {
		t.Fatalf("a null observed_at decoded as %v, want the zero time", decoded.LastSerials[1].ObservedAt)
	}
}

func TestSummaryEntriesEmptyResultDecodesToEmptySlices(t *testing.T) {
	t.Parallel()

	live, err := ReadTerraformStateAdminEvidence(context.Background(), summaryFakeQueryer(nil, nil), statuspkg.MaxTerraformStateRecentWarnings, summaryObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := SummaryEntries(context.Background(), summaryFakeQueryer(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %#v, want an empty non-nil slice", entries)
	}
	decoded, err := DecodeSummaryEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, live) || decoded.LastSerials == nil || decoded.RecentWarnings == nil {
		t.Fatalf("decoded = %#v, want the live read %#v with empty non-nil slices", decoded, live)
	}
}

func TestSummaryEntriesSkipRowsTheLiveReadSkips(t *testing.T) {
	t.Parallel()

	serials := [][]any{
		summarySerialRow("hash-a", "not-a-number", sql.NullTime{Time: summaryObservedAt, Valid: true}),
		summarySerialRow("hash-b", "9", sql.NullTime{Time: summaryObservedAt, Valid: true}),
	}
	entries, err := SummaryEntries(context.Background(), summaryFakeQueryer(serials, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: the live read skips a malformed serial", len(entries))
	}
}

func TestSummaryEntriesPropagateAQueryError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	queryer := &fake.ExecQueryer{QueryResponses: []fake.Rows{{FailWith: boom}}}
	if _, err := SummaryEntries(context.Background(), queryer); !errors.Is(err, boom) {
		t.Fatalf("SummaryEntries() error = %v, want %v", err, boom)
	}
}

func TestDecodeSummaryEntriesRejectsWhatItDoesNotUnderstand(t *testing.T) {
	t.Parallel()

	good := `{"safe_locator_hash":"h","backend_kind":"s3","lineage":"l","serial":3,"generation_id":"g","observed_at":"2026-10-07T09:30:15Z"}`
	for name, entries := range map[string][]summary.Entry{
		"unknown section":       {{Section: "mystery", Ordinal: 1, JSON: good}},
		"not json":              {{Section: "last_serial", Ordinal: 1, JSON: `not json`}},
		"unknown field":         {{Section: "last_serial", Ordinal: 1, JSON: strings.Replace(good, `"serial":3`, `"serial":3,"extra":1`, 1)}},
		"serial is text":        {{Section: "last_serial", Ordinal: 1, JSON: strings.Replace(good, `"serial":3`, `"serial":"3"`, 1)}},
		"bad timestamp":         {{Section: "last_serial", Ordinal: 1, JSON: strings.Replace(good, "2026-10-07T09:30:15Z", "yesterday", 1)}},
		"warning with a serial": {{Section: "recent_warning", Ordinal: 1, JSON: good}},
		"trailing data":         {{Section: "last_serial", Ordinal: 1, JSON: good + ` {}`}},
	} {
		if _, err := DecodeSummaryEntries(entries); err == nil {
			t.Fatalf("%s: DecodeSummaryEntries() error = nil, want a decode error", name)
		}
	}
}

func TestSummarySourceSHA256CoversBothStatementsAndTheLimit(t *testing.T) {
	t.Parallel()

	base := summarySourceSHA256("select 24", "select 25", 50)
	if base != summarySourceSHA256("select 24", "select 25", 50) {
		t.Fatal("the digest is not deterministic")
	}
	for name, other := range map[string]string{
		"serial statement":  summarySourceSHA256("select 24 ", "select 25", 50),
		"warning statement": summarySourceSHA256("select 24", "select 25 ", 50),
		"limit":             summarySourceSHA256("select 24", "select 25", 51),
		"concatenation":     summarySourceSHA256("select 2", "4select 25", 50),
	} {
		if other == base {
			t.Fatalf("changing the %s did not change the digest", name)
		}
	}
	if got := SummarySourceSHA256(); got != summarySourceSHA256(terraformStateLastSerialQuery, terraformStateRecentWarningsQuery, statuspkg.MaxTerraformStateRecentWarnings) || len(got) != 64 {
		t.Fatalf("SummarySourceSHA256() = %q, want the digest of the two shipped statements and the production limit", got)
	}
}
