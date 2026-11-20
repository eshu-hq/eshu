// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Commit-listener operator variables. The registry declares them
// (go/internal/envregistry); this file reads them. Unset ESHU_COMMIT_LISTEN
// keeps the listener dark.
const (
	commitEnvListen       = "ESHU_COMMIT_LISTEN"
	commitEnvManifestPath = "ESHU_COMMIT_MANIFEST_PATH"
	commitEnvGrantsPath   = "ESHU_COMMIT_GRANTS_PATH"
	commitEnvClaimPlane   = "ESHU_COMMIT_CLAIM_PLANE_URL"
	commitEnvSourceSystem = "ESHU_COMMIT_SOURCE_SYSTEM"
	commitEnvScopeKind    = "ESHU_COMMIT_SCOPE_KIND"
	commitEnvInstanceID   = "ESHU_COMMIT_INSTANCE_ID"
)

// commitListenerClaimPlaneTimeout bounds one claim-plane call the listener
// makes. It matches the store fence's per-pin bound
// (DefaultClaimFencePinAttemptTimeout), so whichever fires first, a hung
// plane is a failed attempt, never a stuck commit.
const commitListenerClaimPlaneTimeout = 10 * time.Second

// commitListenerConfig carries one fully-validated commit-listener wiring.
// A nil *commitListenerConfig means the lane is dark: the operator set no
// listener address, so nothing is served and no commit traffic is possible.
type commitListenerConfig struct {
	listenAddr    string
	manifestPath  string
	grantsPath    string
	claimPlaneURL string
	sourceSystem  string
	scopeKind     scope.ScopeKind
	instanceID    string
}

// commitListenerConfigFromEnv reads the commit-listener contract: unset
// ESHU_COMMIT_LISTEN returns nil (the lane stays dark); a set address
// requires every companion variable and a literal listen endpoint. Any
// half-wired or malformed contract fails closed before anything serves,
// except ESHU_COMMIT_SCOPE_KIND, which is presence-checked only: an
// unlisted spelling passes startup and is refused per-request by the
// admission match. There is no closed kind list in-repo
// (IngestionScope.Validate admits any non-blank kind), so startup cannot
// tell a typo from a kind added after this binary was built; the same
// presence-only check shipped in the pre-split monolith listener.
func commitListenerConfigFromEnv(getenv func(string) string) (*commitListenerConfig, error) {
	if getenv == nil {
		return nil, nil
	}
	listenAddr := strings.TrimSpace(getenv(commitEnvListen))
	if listenAddr == "" {
		return nil, nil
	}
	if err := validateCommitListenAddr(listenAddr); err != nil {
		return nil, err
	}
	cfg := &commitListenerConfig{listenAddr: listenAddr}
	// Fixed declaration order: the missing-variable error must read the
	// same every run, so companions are collected from an ordered slice,
	// never a map range.
	ordered := []struct {
		env    string
		assign *string
	}{
		{commitEnvManifestPath, &cfg.manifestPath},
		{commitEnvGrantsPath, &cfg.grantsPath},
		{commitEnvClaimPlane, &cfg.claimPlaneURL},
		{commitEnvSourceSystem, &cfg.sourceSystem},
		{commitEnvInstanceID, &cfg.instanceID},
	}
	var missing []string
	for _, field := range ordered {
		value := strings.TrimSpace(getenv(field.env))
		if value == "" && field.env != commitEnvGrantsPath {
			missing = append(missing, field.env)
			continue
		}
		*field.assign = value
	}
	if raw := strings.TrimSpace(getenv(commitEnvScopeKind)); raw == "" {
		missing = append(missing, commitEnvScopeKind)
	} else {
		cfg.scopeKind = scope.ScopeKind(raw)
	}
	if len(missing) > 0 {
		// Sorted so the error reads the same every run no matter which
		// companions are missing.
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"commit listener %s requires %s to be set",
			commitEnvListen,
			strings.Join(missing, ", "),
		)
	}
	if err := validateCommitClaimPlaneURL(cfg.claimPlaneURL); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validateCommitListenAddr admits only the endpoints the satellite proxy
// dials: an empty host (all interfaces) or a literal IP with a valid port.
// Hostnames are refused: they would resolve at dial time to an endpoint the
// deployment never reviewed.
func validateCommitListenAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("commit listener %s: invalid listen address %q: %w", commitEnvListen, addr, err)
	}
	if strings.TrimSpace(host) != "" && net.ParseIP(strings.Trim(host, "[]")) == nil {
		return fmt.Errorf(
			"commit listener %s: host must be empty or a literal IP, got %q",
			commitEnvListen,
			host,
		)
	}
	portNum, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("commit listener %s: invalid port %q", commitEnvListen, port)
	}
	return nil
}

// validateCommitClaimPlaneURL admits only a bare base URL: http or https
// with a host and no user info, path, query, or fragment. Anything else
// fails startup closed instead of fencing commits against the wrong plane.
func validateCommitClaimPlaneURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("commit listener %s: invalid claim-plane URL %q: %w", commitEnvClaimPlane, raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf(
			"commit listener %s: claim-plane URL scheme must be http or https, got %q",
			commitEnvClaimPlane,
			parsed.Scheme,
		)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf(
			"commit listener %s: claim-plane URL must be a bare base URL with no user, path, query, or fragment",
			commitEnvClaimPlane,
		)
	}
	if path := strings.TrimSuffix(parsed.Path, "/"); path != "" {
		return fmt.Errorf(
			"commit listener %s: claim-plane URL must carry no path, got %q",
			commitEnvClaimPlane,
			parsed.Path,
		)
	}
	return nil
}

// loadCommitGrants decodes the optional producer-grants file: either a bare
// JSON array of grants or an object holding them under "grants". An empty
// path pairs nothing, so namespaced-only manifests need no file.
func loadCommitGrants(path string) ([]component.ProducerGrant, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied config path, same trust as the manifest path.
	if err != nil {
		return nil, fmt.Errorf("commit listener %s: read grants: %w", commitEnvGrantsPath, err)
	}
	var grants []component.ProducerGrant
	if err := json.Unmarshal(raw, &grants); err == nil {
		return grants, nil
	}
	var wrapped struct {
		Grants []component.ProducerGrant `json:"grants"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("commit listener %s: decode grants: %w", commitEnvGrantsPath, err)
	}
	return wrapped.Grants, nil
}

// newCommitListenerMux builds the serving mux for POST
// /workflow.v1/CommitClaimFacts from one validated bundle: the claim-plane
// client the fence probes, the fenced committer that persists, and the
// manifest plus grants admission checks results against. The route comes
// from the shared protocol table, never a local literal, so the listener
// serves exactly the operation the released satellite proxy forwards to.
func newCommitListenerMux(
	store collector.ClaimControlStore,
	committer collector.Committer,
	manifest component.Manifest,
	grants []component.ProducerGrant,
	sourceSystem string,
	scopeKind scope.ScopeKind,
	instanceID string,
) (*http.ServeMux, error) {
	handler, err := extensionhost.NewCommitHandler(extensionhost.CommitHandlerConfig{
		Manifest:            manifest,
		Grants:              grants,
		SourceSystem:        sourceSystem,
		ScopeKind:           scopeKind,
		CollectorInstanceID: instanceID,
		ControlStore:        store,
		Committer:           committer,
	})
	if err != nil {
		return nil, fmt.Errorf("commit listener: %w", err)
	}
	return extensionhost.NewCommitClaimFactsMux(handler), nil
}

// buildCommitListenerMux wires the production commit bundle: the optional
// grants and the manifest from their files, the claim-plane client the
// fence probes and pins through, and the ingestion store with its
// claim-plane fence. Grants load first so the manifest validates against
// them: a core-owned kind with a covering grant starts, without one it
// fails closed. The fence pairing is unconditional: a claim the plane
// holds cannot commit through the in-tree path, so the store always
// fences on the same client that serves as the control store.
func buildCommitListenerMux(
	cfg *commitListenerConfig,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	grants, err := loadCommitGrants(cfg.grantsPath)
	if err != nil {
		return nil, err
	}
	manifest, err := component.LoadManifestWithGrants(cfg.manifestPath, grants)
	if err != nil {
		return nil, fmt.Errorf("commit listener %s: %w", commitEnvManifestPath, err)
	}
	planeClient := workflow.NewClaimPlaneClient(
		cfg.claimPlaneURL,
		&http.Client{Timeout: commitListenerClaimPlaneTimeout},
	)
	store := pgstorage.NewIngestionStore(database)
	store.ClaimPlane = &pgstorage.ClaimPlaneFence{Pinner: planeClient}
	store.Logger = logger
	store.Instruments = instruments
	return newCommitListenerMux(
		planeClient,
		store,
		manifest,
		grants,
		cfg.sourceSystem,
		cfg.scopeKind,
		cfg.instanceID,
	)
}

// startCoreCommitListener reads the listener contract and, when enabled,
// wires the commit bundle and starts the commit server on its own
// goroutine. It returns a nil channel when the lane is dark (an unset
// ESHU_COMMIT_LISTEN), so the caller's select blocks on it forever in
// exactly the no-listener case. A half-wired contract fails here, before
// the primary server opens.
func startCoreCommitListener(
	ctx context.Context,
	getenv func(string) string,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (<-chan error, error) {
	cfg, err := commitListenerConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}
	mux, err := buildCommitListenerMux(
		cfg,
		coreInstrumentedDB(rawDB, instruments, "commit_listener"),
		instruments,
		logger,
	)
	if err != nil {
		return nil, err
	}
	ch := make(chan error, 1)
	go func() {
		ch <- runCommitListener(ctx, cfg, mux, logger, coreShutdownTimeout(getenv))
	}()
	return ch, nil
}

// runCommitListener serves the commit mux until the process context ends,
// then drains gracefully under the shared shutdown deadline. It maps the
// expected closed-server error to nil so a graceful stop is not a failure.
func runCommitListener(
	ctx context.Context,
	cfg *commitListenerConfig,
	handler http.Handler,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
) error {
	server := &http.Server{Addr: cfg.listenAddr, Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	logger.Info("commit listener listening",
		telemetry.EventAttr("runtime.commit_listener.listening"),
		slog.String("addr", cfg.listenAddr),
	)
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if !coreServeFailed(err) {
			return nil
		}
		return err
	}
}
