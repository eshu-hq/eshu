// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"io"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestNewRouterWiresGovernanceAuditStoreLogger is the #6574 review-fix
// regression (R2 P2-4): GovernanceAuditStore.List warns once per field when
// a page holds an enum value this build does not know, through the store's
// Logger or slog.Default when unset. cmd/api never calls slog.SetDefault, so
// a store built without the API's logger sends that warn to Go's default
// text handler on stderr instead of the JSON log every other API signal
// uses. Like TestNewRouterWiresCodeLogger, the reflective sweep cannot see
// a *slog.Logger field, so this test drives newRouter with a distinguishable
// logger and asserts pointer identity on both stores the API builds: the
// admin reader's List store (where the warn fires) and the summary store
// newRouter builds when none is passed in.
func TestNewRouterWiresGovernanceAuditStoreLogger(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", "postgres://example.invalid/eshu")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	router, err := newRouter(
		db,
		query.NewNeo4jReader(nil, ""),
		query.NewContentReader(db),
		staticStatusReader{},
		staticMetricsSource{},
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		logger,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
		query.CookieSecureAuto,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	if router.AdminIdentityReads == nil {
		t.Fatal("newRouter().AdminIdentityReads = nil, want wired")
	}

	reader, ok := router.AdminIdentityReads.Audit.(*adminGovernanceAuditReader)
	if !ok {
		t.Fatalf("newRouter().AdminIdentityReads.Audit = %T, want *adminGovernanceAuditReader", router.AdminIdentityReads.Audit)
	}
	if reader.store.Logger != logger {
		t.Errorf("admin audit reader store Logger = %p, want the passed logger %p (the unknown-enum warn would land on stderr as text, not in the API JSON log)", reader.store.Logger, logger)
	}

	summary, ok := reader.summary.(pgstatus.GovernanceAuditStore)
	if !ok {
		t.Fatalf("newRouter() governance audit summary = %T, want pgstatus.GovernanceAuditStore", reader.summary)
	}
	if summary.Logger != logger {
		t.Errorf("governance audit summary store Logger = %p, want the passed logger %p", summary.Logger, logger)
	}
}
