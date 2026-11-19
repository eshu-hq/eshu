// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
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

// newCoreMetricsSource resolves the Prometheus/Mimir time-series source
// from the collector inventory. No enabled target yields a nil source and
// the metrics routes fail closed, matching the retired wiring.
func newCoreMetricsSource(getenv func(string) string) (MetricsTimeSeriesSource, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(
		strings.TrimSpace(getenv("ESHU_COLLECTOR_INSTANCES_JSON")))
	if err != nil {
		return nil, fmt.Errorf("parse ESHU_COLLECTOR_INSTANCES_JSON: %w", err)
	}
	instance, ok, err := selectCoreMetricsCollectorInstance(
		instances, strings.TrimSpace(getenv("ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID")))
	if err != nil || !ok {
		return nil, err
	}
	target, ok, err := selectCoreMetricsTimeSeriesTarget(instance, getenv)
	if err != nil || !ok {
		return nil, err
	}
	return NewPrometheusMetricsTimeSeriesSource(target)
}

// coreMetricsCollectorConfiguration mirrors the thin CLI collector inventory
// shape the metrics target resolution reads.
type coreMetricsCollectorConfiguration struct {
	Targets []coreMetricsTargetConfiguration `json:"targets"`
}

// coreMetricsTargetConfiguration is one Prometheus/Mimir target entry.
type coreMetricsTargetConfiguration struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	PathPrefix  string `json:"path_prefix"`
	TokenEnv    string `json:"token_env"`
	TenantID    string `json:"tenant_id"`
	TenantIDEnv string `json:"tenant_id_env"`
	Enabled     bool   `json:"enabled"`
}

func selectCoreMetricsCollectorInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, bool, error) {
	var matches []workflow.DesiredCollectorInstance
	var requestedMatched bool
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorPrometheusMimir {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		requestedMatched = true
		if !instance.Enabled {
			continue
		}
		matches = append(matches, instance)
	}
	if requestedInstanceID != "" && !requestedMatched {
		return workflow.DesiredCollectorInstance{}, false, fmt.Errorf(
			"prometheus/mimir collector instance %q not found", requestedInstanceID)
	}
	switch len(matches) {
	case 0:
		return workflow.DesiredCollectorInstance{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return workflow.DesiredCollectorInstance{}, false, fmt.Errorf(
			"multiple prometheus/mimir collector instances configured; set ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID")
	}
}

func selectCoreMetricsTimeSeriesTarget(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (PrometheusMetricsTimeSeriesConfig, bool, error) {
	var config coreMetricsCollectorConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &config); err != nil {
		return PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf(
			"decode prometheus/mimir collector configuration: %w", err)
	}
	var matches []PrometheusMetricsTimeSeriesConfig
	for i, target := range config.Targets {
		if !target.Enabled {
			continue
		}
		mapped, err := mapCoreMetricsTimeSeriesTarget(target, getenv)
		if err != nil {
			return PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf("targets[%d]: %w", i, err)
		}
		matches = append(matches, mapped)
	}
	switch len(matches) {
	case 0:
		return PrometheusMetricsTimeSeriesConfig{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return PrometheusMetricsTimeSeriesConfig{}, false, fmt.Errorf(
			"multiple enabled prometheus/mimir targets configured for %q", instance.InstanceID)
	}
}

func mapCoreMetricsTimeSeriesTarget(
	target coreMetricsTargetConfiguration,
	getenv func(string) string,
) (PrometheusMetricsTimeSeriesConfig, error) {
	provider := strings.ToLower(strings.TrimSpace(target.Provider))
	if provider != "prometheus" && provider != "mimir" {
		return PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("provider must be prometheus or mimir")
	}
	token := strings.TrimSpace(target.TokenEnv)
	if token != "" {
		value := strings.TrimSpace(getenv(token))
		if value == "" {
			return PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("token_env %s did not resolve a value", token)
		}
		token = value
	}
	tenantID := strings.TrimSpace(target.TenantID)
	if envName := strings.TrimSpace(target.TenantIDEnv); envName != "" {
		value := strings.TrimSpace(getenv(envName))
		if value == "" {
			return PrometheusMetricsTimeSeriesConfig{}, fmt.Errorf("tenant_id_env %s did not resolve a value", envName)
		}
		tenantID = value
	}
	return PrometheusMetricsTimeSeriesConfig{
		BaseURL:    strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		PathPrefix: strings.TrimSpace(target.PathPrefix),
		Token:      token,
		TenantID:   tenantID,
	}, nil
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
