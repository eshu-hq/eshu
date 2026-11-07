// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel"
)

// Commit listener environment contract. ESHU_COMMIT_LISTEN is the gate:
// unset (or blank) keeps the core dark and nothing else is read. When set,
// every other variable is required except ESHU_COMMIT_GRANTS_PATH, which
// stays optional so a manifest-only producer can pair. The address binds
// exactly as given: the satellite plane is a separate pod, so rewriting
// the host would break the only legitimate caller.
const (
	envCommitListen        = "ESHU_COMMIT_LISTEN"
	envCommitManifestPath  = "ESHU_COMMIT_MANIFEST_PATH"
	envCommitGrantsPath    = "ESHU_COMMIT_GRANTS_PATH"
	envCommitClaimPlaneURL = "ESHU_COMMIT_CLAIM_PLANE_URL"
	envCommitSourceSystem  = "ESHU_COMMIT_SOURCE_SYSTEM"
	envCommitScopeKind     = "ESHU_COMMIT_SCOPE_KIND"
	envCommitInstanceID    = "ESHU_COMMIT_INSTANCE_ID"
)

// commitListenTimeout caps one CommitClaimFacts round, matching the
// satellite client default.
const commitListenTimeout = 120 * time.Second

// commitConfig is the file-free half of the listener wiring: manifest and
// grants are already parsed, the producer binding is already validated.
type commitConfig struct {
	addr         string
	manifest     component.Manifest
	grants       []component.ProducerGrant
	claimPlane   string
	sourceSystem string
	scopeKind    scope.ScopeKind
	instanceID   string
}

// normalizeCommitAddr validates the gate value. Blank stays dark. Anything
// else must be a host:port the process can bind; the host is never
// rewritten.
func normalizeCommitAddr(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	host, portText, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "", fmt.Errorf("commit listener: invalid %s %q: %w", envCommitListen, raw, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("commit listener: invalid %s port %q", envCommitListen, portText)
	}
	if address := net.ParseIP(host); host != "" && address == nil {
		return "", fmt.Errorf("commit listener: invalid %s host %q", envCommitListen, host)
	}
	return trimmed, nil
}

// commitEnvError names the missing input so a crashlooping pod says what
// to set.
func commitEnvError(name string) error {
	return fmt.Errorf("commit listener: %s is required when %s is set", name, envCommitListen)
}

// loadGrantsFile parses the optional grants file. An empty path pairs
// nothing; a set path must parse.
func loadGrantsFile(path string) ([]component.ProducerGrant, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("commit listener: %s: read grants %q: %w", envCommitGrantsPath, path, err)
	}
	var grants []component.ProducerGrant
	if err := json.Unmarshal(raw, &grants); err != nil {
		return nil, fmt.Errorf("commit listener: %s: parse grants %q: %w", envCommitGrantsPath, path, err)
	}
	return grants, nil
}

// loadCommitConfig reads the gate first: unset keeps the core dark and
// returns nil without touching files. When set, every binding input must
// be present and parse, or the process fails before opening any socket.
func loadCommitConfig(getenv func(string) string) (*commitConfig, error) {
	addr, err := normalizeCommitAddr(getenv(envCommitListen))
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, nil
	}
	manifestPath := strings.TrimSpace(getenv(envCommitManifestPath))
	if manifestPath == "" {
		return nil, commitEnvError(envCommitManifestPath)
	}
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	grants, err := loadGrantsFile(getenv(envCommitGrantsPath))
	if err != nil {
		return nil, err
	}
	claimPlane := strings.TrimSpace(getenv(envCommitClaimPlaneURL))
	if claimPlane == "" {
		return nil, commitEnvError(envCommitClaimPlaneURL)
	}
	sourceSystem := strings.TrimSpace(getenv(envCommitSourceSystem))
	if sourceSystem == "" {
		return nil, commitEnvError(envCommitSourceSystem)
	}
	scopeText := strings.TrimSpace(getenv(envCommitScopeKind))
	if scopeText == "" {
		return nil, commitEnvError(envCommitScopeKind)
	}
	instanceID := strings.TrimSpace(getenv(envCommitInstanceID))
	if instanceID == "" {
		return nil, commitEnvError(envCommitInstanceID)
	}
	return &commitConfig{
		addr:         addr,
		manifest:     manifest,
		grants:       grants,
		claimPlane:   claimPlane,
		sourceSystem: sourceSystem,
		scopeKind:    scope.ScopeKind(scopeText),
		instanceID:   instanceID,
	}, nil
}

// buildCommitMux constructs the handler and its mux without opening any
// socket: construction errors surface before ListenAndServe. The store
// mirrors the extension satellite wiring: the ingestion store admits only
// claims the satellite plane fenced first.
func buildCommitMux(cfg *commitConfig, database db.ExecQueryer, claimPlaneURL string, logger *slog.Logger) (*http.ServeMux, error) {
	plane := workflow.NewClaimPlaneClient(claimPlaneURL, &http.Client{Timeout: commitListenTimeout})
	store := postgres.NewIngestionStore(database)
	store.Logger = logger
	store.ClaimPlane = &postgres.ClaimPlaneFence{Pinner: plane}
	handler, err := extensionhost.NewCommitHandler(extensionhost.CommitHandlerConfig{
		Manifest:            cfg.manifest,
		Grants:              cfg.grants,
		SourceSystem:        cfg.sourceSystem,
		ScopeKind:           cfg.scopeKind,
		CollectorInstanceID: cfg.instanceID,
		ControlStore:        plane,
		Committer:           store,
	})
	if err != nil {
		return nil, err
	}
	return extensionhost.NewCommitClaimFactsMux(handler), nil
}

// openCommitAccess opens the same role-attested pool the API uses. It is a
// seam so the wiring stays testable without a database.
var openCommitAccess = func(ctx context.Context, getenv func(string) string, logger *slog.Logger) (*pgaccess.Access, error) {
	poolCfg, err := pgaccess.LoadRoleAttestedConfig(getenv, internalruntime.PostgresWorkloadAPI)
	if err != nil {
		return nil, fmt.Errorf("commit listener: load postgres pool config: %w", err)
	}
	poolCfg.Logger = logger
	observer, err := pgaccess.NewObserver(otel.Meter(telemetry.DefaultSignalName), otel.Tracer(telemetry.DefaultSignalName))
	if err != nil {
		return nil, fmt.Errorf("commit listener: register postgres access telemetry: %w", err)
	}
	access, err := pgaccess.Open(ctx, poolCfg, observer)
	if err != nil {
		return nil, fmt.Errorf("commit listener: open postgres access: %w", err)
	}
	return access, nil
}

// commitServeFailed reports whether a ListenAndServe return must fail the
// process. http.ErrServerClosed is the clean shutdown path; any other
// return means the commit socket never served, and running public-only
// would look healthy while the proxy has no core URL.
func commitServeFailed(err error) bool {
	return err != nil && err != http.ErrServerClosed
}

// wireCommitListener builds the serving half of the mount. It returns nil
// without touching the database or the network while the gate env is
// unset. The caller owns starting and stopping the server; the cleanup
// closes the pool.
func wireCommitListener(ctx context.Context, getenv func(string) string, logger *slog.Logger) (*http.Server, func(), error) {
	cfg, err := loadCommitConfig(getenv)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, nil
	}
	access, err := openCommitAccess(ctx, getenv, logger)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if err := access.Close(); err != nil {
			logger.Warn("commit listener pool close failed", "error", err)
		}
	}
	mux, err := buildCommitMux(cfg, db.ExecQueryer(postgres.SQLDB{DB: access.Writer()}), cfg.claimPlane, logger)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}, cleanup, nil
}
