// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
)

// coreEnvOrEmpty reads one operator variable. A nil getenv reads as empty.
func coreEnvOrEmpty(getenv func(string) string, key string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv(key))
}

// CoreGovernanceAuditAppender adapts the governance audit store into the
// append surface the auth chain and admin handlers need. It is exported
// for the core-api binary. A reader without append support yields nil,
// disabling audit recording without failing startup.
func CoreGovernanceAuditAppender(reader GovernanceAuditSummaryReader) GovernanceAuditAppender {
	return coreRecoveryAuditAppender(reader)
}

// coreRecoveryAuditAppender adapts the governance audit reader into the
// append surface admin handlers need. A reader without append support
// yields nil, disabling audit recording without failing startup.
func coreRecoveryAuditAppender(reader GovernanceAuditSummaryReader) GovernanceAuditAppender {
	if appender, ok := reader.(GovernanceAuditAppender); ok {
		return appender
	}
	return nil
}

// CoreGovernanceAuditStore builds the Postgres governance audit store
// shared between handlers. It is exported for the core-api binary, which
// needs the same store for seeding and the auth chain. A nil logger falls
// back to slog.Default.
func CoreGovernanceAuditStore(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) GovernanceAuditSummaryReader {
	if rawDB == nil {
		return nil
	}
	return auditstore.NewGovernanceAuditStore(coreInstrumentedDB(rawDB, instruments, "governance_audit")).WithLogger(logger)
}

// coreInstrumentedDB wraps a writer handle with OTEL tracing when
// instruments are wired; otherwise it returns the plain handle.
func coreInstrumentedDB(rawDB *sql.DB, instruments *telemetry.Instruments, storeName string) db.ExecQueryer {
	inner := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	if instruments == nil {
		return inner
	}
	return &pgstatus.InstrumentedDB{
		Inner:       inner,
		Tracer:      otel.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   storeName,
	}
}

// newCoreStatusReader confines every status read to one guarded snapshot
// transaction built through the process-wide summary reader.
func newCoreStatusReader(
	reader db.ReadStore,
	instruments *telemetry.Instruments,
	getenv func(string) string,
) (status.Reader, error) {
	summaryReader, err := pgstatus.NewStatusSummaryReader(getenvOrFunc(getenv))
	if err != nil {
		return nil, fmt.Errorf("configure status summary reader: %w", err)
	}
	return pgaccess.NewSnapshotStatusReader(reader, func(queryer db.Queryer) status.Reader {
		return newCoreStatusStore(queryer, instruments, summaryReader)
	}, otel.Tracer(telemetry.DefaultSignalName)), nil
}

// newCoreStatusStore wires the shared meter-provider instruments onto the
// operator status-serving StatusStore so the per-read status metric emits.
func newCoreStatusStore(
	queryer db.Queryer,
	instruments *telemetry.Instruments,
	summaryReader *pgstatus.StatusSummaryReader,
) pgstatus.StatusStore {
	store := pgstatus.NewStatusStore(queryer).WithSummaryReader(summaryReader)
	store.Instruments = instruments
	return store
}

// getenvOrFunc normalizes a possibly-nil getenv for readers that require one.
func getenvOrFunc(getenv func(string) string) func(string) string {
	if getenv == nil {
		return func(string) string { return "" }
	}
	return getenv
}

// loadCoreSSMHostBounds reads the fail-closed SSM host freshness bounds.
// A missing or invalid value zeroes the bounds so Validate fails and the
// handler answers 503 rather than serving under unbounded rules.
func loadCoreSSMHostBounds(getenv func(string) string) ssmhost.Bounds {
	if getenv == nil {
		return ssmhost.Bounds{}
	}
	return ssmhost.Bounds{
		MaxCaptureAge:             strictCorePositiveDuration(getenv("ESHU_SSM_HOST_MAX_CAPTURE_AGE")),
		MaxCrossTypeSkew:          strictCorePositiveDuration(getenv("ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW")),
		MaxPlatformObservationAge: strictCorePositiveDuration(getenv("ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE")),
	}
}

func strictCorePositiveDuration(raw string) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// startCoreInfraSearchKeyRead wires the infra search full-text read onto the
// handler and starts its readiness refresher for the life of the process.
func startCoreInfraSearchKeyRead(
	getenv func(string) string,
	infra *InfraHandler,
	backend GraphBackend,
	graphDriven bool,
	graphReader GraphQuery,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) error {
	if infra == nil {
		return nil
	}
	reader, err := fulltext.Start(context.Background(), coreEnvOrEmpty(getenv, fulltext.ReadEnv),
		backend == GraphBackendNeo4j && graphDriven, graphReader, logger, instruments)
	if err != nil {
		return fmt.Errorf("configure infra search key read: %w", err)
	}
	infra.SearchKey = reader
	return nil
}
