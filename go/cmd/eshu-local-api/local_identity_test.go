// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestTranslateLocalIdentityAPITokenNotFound is the adapter half of issue
// #5164's self-service 404 chain: the storage layer's "no active token matched"
// sentinel must surface to the query layer as query.ErrLocalIdentityAPITokenNotFound
// (which the revoke/rotate handlers turn into a non-disclosing 404), while every
// other error passes through unchanged so real failures are not masked as 404.
func TestTranslateLocalIdentityAPITokenNotFound(t *testing.T) {
	t.Parallel()

	if got := translateLocalIdentityAPITokenNotFound(nil); got != nil {
		t.Fatalf("translate(nil) = %v, want nil", got)
	}

	// The storage sentinel — including when wrapped — maps to the query sentinel.
	if got := translateLocalIdentityAPITokenNotFound(pgstatus.ErrLocalIdentityAPITokenUnavailable); !errors.Is(got, query.ErrLocalIdentityAPITokenNotFound) {
		t.Fatalf("translate(sentinel) = %v, want query.ErrLocalIdentityAPITokenNotFound", got)
	}
	wrapped := fmt.Errorf("revoke local identity api token: %w", pgstatus.ErrLocalIdentityAPITokenUnavailable)
	if got := translateLocalIdentityAPITokenNotFound(wrapped); !errors.Is(got, query.ErrLocalIdentityAPITokenNotFound) {
		t.Fatalf("translate(wrapped sentinel) = %v, want query.ErrLocalIdentityAPITokenNotFound", got)
	}

	// An unrelated error passes through so a real DB failure is not hidden as a 404.
	other := errors.New("connection reset")
	if got := translateLocalIdentityAPITokenNotFound(other); !errors.Is(got, other) {
		t.Fatalf("translate(other) = %v, want the original error", got)
	}
	if errors.Is(translateLocalIdentityAPITokenNotFound(other), query.ErrLocalIdentityAPITokenNotFound) {
		t.Fatalf("translate(other) must not masquerade as not-found")
	}
}
