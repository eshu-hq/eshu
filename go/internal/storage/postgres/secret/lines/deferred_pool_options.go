// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// PostgresOpener opens the two Postgres pools of the deferred-writer
// composition. RuntimePostgresOpener is the production implementation; a test
// substitutes a recorder to pin which opener each pool uses.
type PostgresOpener interface {
	// Open opens a plain pool with no connect hook and no extra session SQL.
	Open(ctx context.Context, getenv func(string) string) (*sql.DB, error)
	// OpenWithSessionHooks opens a pool whose every connection runs hooks and
	// extraSessionSQL after the runtime's own session setup.
	OpenWithSessionHooks(ctx context.Context, getenv func(string) string, hooks []inventory.ConnectHook, extraSessionSQL ...string) (*sql.DB, error)
}

// RuntimePostgresOpener opens pools through the runtime's Postgres openers,
// which read the DSN and pool bounds from the environment.
type RuntimePostgresOpener struct{}

// Open opens a plain bootstrap pool with a per-physical owner-role check.
func (RuntimePostgresOpener) Open(ctx context.Context, getenv func(string) string) (*sql.DB, error) {
	return runtimecfg.OpenPostgresForWorkload(ctx, getenv, runtimecfg.PostgresWorkloadBootstrap)
}

// OpenWithSessionHooks opens a runtime pool with per-connection hooks
// after the bootstrap owner-role check.
func (RuntimePostgresOpener) OpenWithSessionHooks(ctx context.Context, getenv func(string) string, hooks []inventory.ConnectHook, extraSessionSQL ...string) (*sql.DB, error) {
	return runtimecfg.OpenPostgresForWorkloadWithSessionHooks(ctx, getenv, runtimecfg.PostgresWorkloadBootstrap, hooks, extraSessionSQL...)
}

// BootstrapDeferredPoolOptions is the one composition of OpenDeferredWriterPool
// that bootstrap-index's openBootstrapDB and the enrolled live tests both call,
// so a test cannot drift from production (#8833 F14).
//
// The probe pool is plain: it carries no connect hook and no deferred session
// SQL, so it takes (5318,2) shared only through the probe's own polling try. A
// hooked probe would wait inside the startup ping and reopen F8. It holds one
// connection. The writer pool runs fence.Hook() on every connection (the
// (5318,2) shared lock and the birth fence) plus DeferredSessionSQL. Dropping
// the hook reopens F1: pool sessions would take no (5318,2) and get no fence.
//
// Fence on the returned options is the fence the hook is built from; the
// caller hands that same object to the run lock (#8833 F18).
//
// Poll and LogEvery stay zero (their defaults); a test may set them on the
// returned options.
func BootstrapDeferredPoolOptions(opener PostgresOpener, getenv func(string) string, fence *RunFence, wait time.Duration, logger *slog.Logger) DeferredPoolOptions {
	return DeferredPoolOptions{
		Probe: func(ctx context.Context) (*sql.DB, error) {
			pool, err := opener.Open(ctx, getenv)
			if err != nil {
				return nil, err
			}
			pool.SetMaxOpenConns(1)
			return pool, nil
		},
		Open: func(ctx context.Context) (*sql.DB, error) {
			return opener.OpenWithSessionHooks(ctx, getenv,
				[]inventory.ConnectHook{fence.Hook()}, DeferredSessionSQL)
		},
		Fence:  fence,
		Wait:   wait,
		Logger: logger,
	}
}
