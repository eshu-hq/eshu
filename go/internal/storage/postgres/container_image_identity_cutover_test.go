// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestContainerImageIdentityCutoverExists(t *testing.T) {
	t.Parallel()

	queryer := &containerImageIdentityCutoverQueryer{
		rows: &containerImageIdentityCutoverRows{exists: true, hasRow: true},
	}
	store := NewContainerImageIdentityCutoverStore(queryer)

	exists, err := store.ContainerImageIdentityCutoverExists(
		context.Background(),
		"repository:synthetic",
		"generation-5854",
	)
	if err != nil {
		t.Fatalf("ContainerImageIdentityCutoverExists() error = %v", err)
	}
	if !exists {
		t.Fatal("ContainerImageIdentityCutoverExists() = false, want true")
	}
	if got, want := queryer.query, containerImageIdentityCutoverExistsQuery; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if got, want := fmt.Sprint(queryer.args), "[repository:synthetic generation-5854]"; got != want {
		t.Fatalf("query args = %s, want %s", got, want)
	}
	if !queryer.rows.closed {
		t.Fatal("cutover rows were not closed")
	}
}

func TestContainerImageIdentityCutoverExistsFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		queryer Queryer
		want    string
	}{
		{
			name:    "nil queryer",
			queryer: nil,
			want:    "queryer is required",
		},
		{
			name: "query error",
			queryer: &containerImageIdentityCutoverQueryer{
				err: errors.New("synthetic query failure"),
			},
			want: "synthetic query failure",
		},
		{
			name: "no row",
			queryer: &containerImageIdentityCutoverQueryer{
				rows: &containerImageIdentityCutoverRows{},
			},
			want: "returned no row",
		},
		{
			name: "scan error",
			queryer: &containerImageIdentityCutoverQueryer{
				rows: &containerImageIdentityCutoverRows{
					hasRow:  true,
					scanErr: errors.New("synthetic scan failure"),
				},
			},
			want: "synthetic scan failure",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := NewContainerImageIdentityCutoverStore(tt.queryer)
			_, err := store.ContainerImageIdentityCutoverExists(
				context.Background(),
				"repository:synthetic",
				"generation-5854",
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf(
					"ContainerImageIdentityCutoverExists() error = %v, want %q",
					err,
					tt.want,
				)
			}
		})
	}
}

func TestContainerImageIdentityLegacyCleanupComplete(t *testing.T) {
	t.Parallel()

	queryer := &containerImageIdentityCutoverQueryer{
		rows: &containerImageIdentityCutoverRows{exists: true, hasRow: true},
	}
	store := NewContainerImageIdentityCutoverStore(queryer)
	complete, err := store.ContainerImageIdentityLegacyCleanupComplete(
		context.Background(),
		"repository:synthetic",
		"generation-5854",
	)
	if err != nil {
		t.Fatalf("ContainerImageIdentityLegacyCleanupComplete() error = %v", err)
	}
	if !complete {
		t.Fatal("ContainerImageIdentityLegacyCleanupComplete() = false, want true")
	}
	if queryer.query != containerImageIdentityLegacyCleanupCompleteQuery {
		t.Fatalf("legacy cleanup query = %q", queryer.query)
	}
	for _, want := range []string{
		"COALESCE(payload->>'identity_format', '') <> 'image_ref_v2'",
		"ORDER BY fact_id",
		"LIMIT 1",
	} {
		if !strings.Contains(queryer.query, want) {
			t.Fatalf("legacy cleanup query missing %q", want)
		}
	}
}

type containerImageIdentityCutoverQueryer struct {
	rows  *containerImageIdentityCutoverRows
	err   error
	query string
	args  []any
}

func (q *containerImageIdentityCutoverQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (Rows, error) {
	q.query = query
	q.args = append([]any(nil), args...)
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

type containerImageIdentityCutoverRows struct {
	exists  bool
	hasRow  bool
	scanErr error
	closed  bool
}

func (r *containerImageIdentityCutoverRows) Next() bool {
	if !r.hasRow {
		return false
	}
	r.hasRow = false
	return true
}

func (r *containerImageIdentityCutoverRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan destination count = %d, want 1", len(dest))
	}
	target, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("scan destination type = %T, want *bool", dest[0])
	}
	*target = r.exists
	return nil
}

func (*containerImageIdentityCutoverRows) Err() error { return nil }

func (r *containerImageIdentityCutoverRows) Close() error {
	r.closed = true
	return nil
}
