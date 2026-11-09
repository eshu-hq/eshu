// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The access-backed fleet pins one qualified member per tick and runs every
// statement on that member only. These tests prove the fail-closed edges
// without a live standby; the qualified-member path proves on QA (L5).
func TestStagedAuditAccessFleetNilAccessFailsClosed(t *testing.T) {
	fleet := NewStagedAuditAccessFleet(nil)
	if _, err := fleet.PinMember(context.Background()); err == nil {
		t.Fatal("PinMember on nil access succeeded, want fail-closed error")
	}
}

func TestStagedAuditAccessFleetEmptyAccessFailsClosed(t *testing.T) {
	fleet := NewStagedAuditAccessFleet(&Access{})
	if _, err := fleet.PinMember(context.Background()); err == nil {
		t.Fatal("PinMember on memberless access succeeded, want fail-closed error")
	}
}

func TestStagedAuditAccessFleetUnknownMemberAborts(t *testing.T) {
	fleet := NewStagedAuditAccessFleet(&Access{})
	err := fleet.RunOnMember(context.Background(), "no-such-member", func(context.Context, db.Queryer) error {
		return nil
	})
	if err == nil {
		t.Fatal("RunOnMember on unknown member succeeded, want abort error")
	}
}
