// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command eshu-core-api is the Wave-6 core serving binary (step-3 gating
// ruling D1). It serves the full forwarded query surface from stores
// directly: flag parsing, store wiring, and ListenAndServe only. Route
// registration, the auth middleware chain, and handler wiring live in
// go/internal/query (serve.go); this package mounts only the
// import-cycle-forced extras (CLI-support handlers, the service-intelligence
// report, and the ask engine) on the constructor's bare mux.
//
// One-way no-loop invariant (ruling D3): this binary takes no
// --core-base-url flag and never dials any API backend. Traffic flows one
// way, satellite into this Service. The satellite keeps its own
// --core-base-url flag; pointing this binary at itself or at another API
// origin is not possible by construction.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/askwiring"
	"github.com/eshu-hq/eshu/go/internal/query"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultCoreAPIAddr = ":8080"
	defaultShutdown    = 30 * time.Second
)

// coreStores bundles the long-lived backend handles serve opens once.
type coreStores struct {
	postgres    *sql.DB
	graphDriver neo4jdriver.DriverWithContext
	graphConfig internalruntime.Neo4jConfig
	telemetry   *telemetry.Providers
	shutdown    func()
}

// openCoreStores bootstraps telemetry and opens Postgres and the pinned
// Neo4j driver. The caller owns the returned cleanup.
func openCoreStores(
	ctx context.Context,
	getenv func(string) string,
	logger *slog.Logger,
) (*coreStores, error) {
	bootstrap, err := telemetry.NewBootstrap("eshu-core-api")
	if err != nil {
		return nil, fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		return nil, fmt.Errorf("telemetry providers: %w", err)
	}
	rawDB, err := internalruntime.OpenPostgres(ctx, getenv)
	if err != nil {
		_ = providers.Shutdown(context.Background())
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	driver, neo4jCfg, err := internalruntime.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		_ = rawDB.Close()
		_ = providers.Shutdown(context.Background())
		return nil, fmt.Errorf("open neo4j driver: %w", err)
	}
	logger.Info("backends connected",
		telemetry.EventAttr("runtime.backends.connected"),
		slog.String("neo4j_uri", neo4jCfg.URI),
		slog.String("neo4j_database", neo4jCfg.DatabaseName))
	return &coreStores{
		postgres:    rawDB,
		graphDriver: driver,
		graphConfig: neo4jCfg,
		telemetry:   providers,
		shutdown: func() {
			_ = driver.Close(context.Background())
			_ = rawDB.Close()
			_ = providers.Shutdown(context.Background())
		},
	}, nil
}

// close releases every backend handle.
func (s *coreStores) close() {
	if s == nil {
		return
	}
	s.shutdown()
}

// resolveCoreProviderKeyring loads the provider-config DEK. A missing DEK
// is not fatal: secret-backed writes fail closed without it. Any other
// keyring error fails startup — a misconfigured DEK must never silently
// degrade to unencrypted operation.
func resolveCoreProviderKeyring(
	getenv func(string) string,
	logger *slog.Logger,
) (*secretcrypto.Keyring, error) {
	keyring, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		if isKeyNotConfigured(err) {
			logger.Info(
				"provider secret encryption key not configured; provider-config secret writes fail closed",
				telemetry.EventAttr("auth.provider_config.keyring_unconfigured"),
			)
			return nil, nil
		}
		return nil, fmt.Errorf("configure provider secret keyring: %w", err)
	}
	return keyring, nil
}

// coreDeferredHandler holds the fully-wrapped handler for the ask engine's
// in-process runner. The engine is built before the final handler exists;
// Set installs it once the auth chain is wrapped.
type coreDeferredHandler struct {
	next http.Handler
}

// Set installs the final handler.
func (h *coreDeferredHandler) Set(next http.Handler) { h.next = next }

// ServeHTTP dispatches through the installed handler.
func (h *coreDeferredHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.next == nil {
		http.Error(w, "core api is starting", http.StatusServiceUnavailable)
		return
	}
	h.next.ServeHTTP(w, r)
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stderr))
}

func run(args []string, getenv func(string) string, stderr *os.File) int {
	fs := flag.NewFlagSet("eshu-core-api", flag.ContinueOnError)
	addr := fs.String("addr", defaultCoreAPIAddr, "listen address (host:port)")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		printCoreAPIVersion(stderr)
		return 0
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := serve(ctx, getenv, logger, *addr); err != nil {
		logger.Error("core api failed", telemetry.EventAttr("runtime.startup.failed"), slog.String("error", err.Error()))
		return 1
	}
	return 0
}

// printCoreAPIVersion prints the build-time version for --version.
func printCoreAPIVersion(stderr *os.File) {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		_, _ = fmt.Fprintln(stderr, "eshu-core-api "+info.Main.Version)
		return
	}
	_, _ = fmt.Fprintln(stderr, "eshu-core-api dev")
}

// serve wires stores, mounts the surface, and runs ListenAndServe until the
// process context ends.
// coreRuntimeConfig parses the required runtime posture: query profile and
// pinned Neo4j backend. Anything else fails startup before stores open.
func coreRuntimeConfig(getenv func(string) string) (query.QueryProfile, query.GraphBackend, error) {
	profile, err := query.ParseQueryProfile(strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")))
	if err != nil {
		return "", "", fmt.Errorf("parse ESHU_QUERY_PROFILE: %w", err)
	}
	if profile == "" {
		profile = query.ProfileProduction
	}
	backend, err := query.ParseGraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
	if err != nil {
		return "", "", fmt.Errorf("parse ESHU_GRAPH_BACKEND: %w", err)
	}
	if backend != query.GraphBackendNeo4j {
		return "", "", fmt.Errorf("eshu-core-api requires the pinned Neo4j backend (ESHU_GRAPH_BACKEND=neo4j), got %q", backend)
	}
	return profile, backend, nil
}

func serve(ctx context.Context, getenv func(string) string, logger *slog.Logger, addr string) error {
	profile, backend, err := coreRuntimeConfig(getenv)
	if err != nil {
		return err
	}

	stores, err := openCoreStores(ctx, getenv, logger)
	if err != nil {
		return err
	}
	defer stores.close()
	rawDB := stores.postgres
	driver := stores.graphDriver
	neo4jCfg := stores.graphConfig
	providers := stores.telemetry

	apiKey, err := internalruntime.ResolveAPIKey(getenv)
	if err != nil {
		return fmt.Errorf("resolve api key: %w", err)
	}
	instruments, err := telemetry.NewInstruments(otel.Meter(telemetry.DefaultSignalName))
	if err != nil {
		return fmt.Errorf("register query instruments: %w", err)
	}
	graphReader := query.NewNeo4jReader(
		driver,
		neo4jCfg.DatabaseName,
		query.WithNeo4jReaderObservability(logger, instruments),
	)

	keyring, err := resolveCoreProviderKeyring(getenv, logger)
	if err != nil {
		return err
	}

	auditStore := query.CoreGovernanceAuditStore(rawDB, instruments, logger)
	appender := query.CoreGovernanceAuditAppender(auditStore)

	identityResolver := scopedtokenPostgresIdentityResolver(rawDB)
	fileResolver, err := scopedtokenFileResolver(getenv)
	if err != nil {
		return fmt.Errorf("resolve scoped token registry: %w", err)
	}
	oidcResolver, err := CoreOIDCBearerResolver(ctx, getenv, rawDB, instruments, logger)
	if err != nil {
		return fmt.Errorf("construct oidc bearer resolver: %w", err)
	}
	enforcement := query.AuthEnforcementConfigured(apiKey, fileResolver, oidcResolver)
	chained := chainResolvers(identityResolver, oidcResolver, fileResolver)

	governance := query.GovernanceStatusConfigFromEnv(getenv, apiKey != "")
	cookieSecure, err := query.ValidateCookieSecureMode(strings.TrimSpace(getenv(query.CookieSecureModeEnv)))
	if err != nil {
		return fmt.Errorf("configure cookie secure mode: %w", err)
	}
	bootstrapMode, err := LoadCoreAuthBootstrapMode(getenv)
	if err != nil {
		return fmt.Errorf("configure setup wizard: %w", err)
	}
	componentPolicy := componentPolicyFromCoreEnv(getenv)

	authWiring, err := buildCoreAuthWiring(
		getenv, rawDB, instruments, keyring, cookieSecure, bootstrapMode, auditStore, logger,
	)
	if err != nil {
		return fmt.Errorf("wire auth handlers: %w", err)
	}

	if err := SeedCoreAdminBootstrap(
		ctx, db.ExecQueryer(pgstatus.SQLDB{DB: rawDB}), getenv, instruments, logger, appender,
	); err != nil {
		return fmt.Errorf("seed initial admin: %w", err)
	}

	// The ask engine dispatches inner tool calls through the fully-wrapped
	// handler; install it once built below.
	inner := &coreDeferredHandler{}
	askResult := askwiring.BuildAskHandler(getenv, inner, apiKey, logger)
	posture := askwiring.BuildNarrationPosture(getenv, askResult.AdapterReady())

	metricsSource, err := newCoreMetricsSource(getenv)
	if err != nil {
		return fmt.Errorf("resolve metrics time-series source: %w", err)
	}

	deps := query.CoreServeDeps{
		DB:                        rawDB,
		Graph:                     graphReader,
		GraphVerify:               driver.VerifyConnectivity,
		GraphDriven:               true,
		GraphBackend:              backend,
		Metrics:                   metricsSource,
		Profile:                   profile,
		APIKey:                    apiKey,
		ScopedTokenResolver:       chained,
		BrowserSessionResolver:    CoreBrowserSessionResolver(rawDB, instruments),
		AuthEnforcementConfigured: enforcement,
		Governance:                governance,
		GovernanceAudit:           appender,
		ComponentHome:             strings.TrimSpace(getenv("ESHU_COMPONENT_HOME")),
		ComponentPolicy:           componentPolicy,
		CookieSecure:              cookieSecure,
		BootstrapMode:             bootstrapMode,
		Getenv:                    getenv,
		Instruments:               instruments,
		Logger:                    logger,
		NarrationPosture:          posture,
		Auth:                      authWiring,
		AdminProofExternal:        strings.TrimSpace(getenv("ESHU_CORE_ADMIN_PROOF_EXTERNAL")) == "1",
		ExtraMounts: []func(*http.ServeMux, *query.APIRouter){
			mountCoreCLISupport,
			mountCoreServiceIntelligence(rawDB, logger,
				query.SupplyChainImpactWinnersReadEnabled(getenv(query.SupplyChainImpactWinnersReadEnv)), instruments),
			mountCoreAsk(askResult.Handler),
			mountCoreMetrics(providers.PrometheusHandler),
		},
	}
	mux, err := query.NewCoreServeMux(deps)
	if err != nil {
		return fmt.Errorf("build serving mux: %w", err)
	}
	inner.Set(mux)

	handler := http.Handler(mux)
	if limiter := CoreOIDCRateLimiter(getenv, instruments); limiter != nil {
		handler = limiter.Middleware(handler)
	}
	handler = wrapCoreAPIMiddleware(handler, readCoreSunsetDate(getenv))

	if err := startCoreOIDCSessionRefresh(ctx, getenv, rawDB, instruments, logger); err != nil {
		return err
	}

	// The commit listener drains on the child context: a primary failure
	// stops it via stopListener, and a process signal stops it via the
	// parent ctx. joinCoreServers owns both directions.
	listenerCtx, stopListener := context.WithCancel(ctx)
	defer stopListener()

	commitErrCh, err := startCoreCommitListener(listenerCtx, getenv, rawDB, instruments, logger)
	if err != nil {
		return err
	}

	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	logger.Info("core api listening",
		telemetry.EventAttr("runtime.server.listening"), slog.String("addr", addr))
	primaryErrCh := make(chan error, 1)
	go func() { primaryErrCh <- server.ListenAndServe() }()
	return joinCoreServers(ctx, stopListener, server, primaryErrCh, commitErrCh, coreShutdownTimeout(getenv))
}

// coreServeFailed reports whether a ListenAndServe return must fail the
// process. http.ErrServerClosed is the clean shutdown path; any other
// return means the socket never served, and running on without it would
// look healthy while the satellite proxy has no core URL.
func coreServeFailed(err error) bool {
	return err != nil && !errors.Is(err, http.ErrServerClosed)
}

// shutdownCoreServer drains the primary server under the shared shutdown
// deadline so in-flight requests complete instead of truncating.
func shutdownCoreServer(primary *http.Server, shutdownTimeout time.Duration) error {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	return primary.Shutdown(shutdownCtx)
}

// awaitCommitResult waits for the commit listener to finish. A nil channel
// means the lane is dark (no listener configured), so there is nothing to
// wait for and the join continues with the primary result alone.
func awaitCommitResult(commitErrCh <-chan error) error {
	if commitErrCh == nil {
		return nil
	}
	return <-commitErrCh
}

// joinCoreServers joins the primary server and the commit listener after
// the first of them — or the process context — finishes, so no return
// abandons the other server mid-flight:
//
//   - ctx done: shut down the primary, await commit completion, return the
//     first fatal error (primary shutdown first, then commit).
//   - primary failed: cancel the listener scope unconditionally — gating
//     the cancel deadlocks when the listener is enabled, because the
//     commit watcher drains on ctx while the join waits for it (#10338) —
//     then await commit completion and report the primary error.
//   - commit failed: shut down the primary, await it, and report the
//     commit error first.
//
// A nil commitErrCh is the dark lane: the select never takes the commit
// branch and the await is a no-op.
func joinCoreServers(
	ctx context.Context,
	cancel context.CancelFunc,
	primary *http.Server,
	primaryErrCh <-chan error,
	commitErrCh <-chan error,
	shutdownTimeout time.Duration,
) error {
	select {
	case <-ctx.Done():
		primaryErr := shutdownCoreServer(primary, shutdownTimeout)
		commitErr := awaitCommitResult(commitErrCh)
		if coreServeFailed(primaryErr) {
			return primaryErr
		}
		if coreServeFailed(commitErr) {
			return commitErr
		}
		return nil
	case err := <-primaryErrCh:
		if coreServeFailed(err) {
			cancel()
		}
		commitErr := awaitCommitResult(commitErrCh)
		if coreServeFailed(err) {
			return err
		}
		return commitErr
	case err := <-commitErrCh:
		cancel()
		primaryErr := shutdownCoreServer(primary, shutdownTimeout)
		if coreServeFailed(err) {
			return err
		}
		return primaryErr
	}
}
