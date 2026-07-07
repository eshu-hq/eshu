// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

const managedHeader = "// SPDX-License-Identifier: MIT\n// Copyright (c) 2025-2026 eshu-hq\n\n// Managed collector entrypoint. Update go/internal/collector/entrypoints/collector_entrypoints.yaml, then rerun scripts/generate-collector-entrypoints.sh.\n\n"

// Generate returns the Go files for a manifest-backed collector entrypoint.
func Generate(manifest Manifest) ([]GeneratedFile, error) {
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	files := []GeneratedFile{
		{Name: "config.go", Contents: mustGenerate(configTemplate, manifest)},
		{Name: "main.go", Contents: mustGenerate(mainTemplate, manifest)},
		{Name: "service.go", Contents: mustGenerate(serviceTemplate, manifest)},
	}
	for i := range files {
		formatted, err := format.Source(files[i].Contents)
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", files[i].Name, err)
		}
		files[i].Contents = formatted
	}
	sort.Slice(files, func(i int, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func validateManifest(manifest Manifest) error {
	var missing []string
	if manifest.SchemaVersion != 1 {
		missing = append(missing, "schema_version")
	}
	requireString(&missing, manifest.CommandDir, "command_dir")
	requireString(&missing, manifest.RuntimeName, "runtime_name")
	requireString(&missing, manifest.BinaryName, "binary_name")
	requireString(&missing, manifest.CollectorLabel, "collector_label")
	requireString(&missing, manifest.GoName, "go_name")
	requireString(&missing, manifest.StoreName, "store_name")
	requireString(&missing, manifest.ClaimIDPrefix, "claim_id_prefix")
	requireString(&missing, manifest.CollectorKindExpr, "collector_kind_expr")
	requireString(&missing, manifest.ScopeKind, "scope_kind")
	requireString(&missing, manifest.AuthMode, "auth_mode")
	requireString(&missing, manifest.TargetListField, "target_list_field")
	requireString(&missing, manifest.Env.CollectorInstances, "env.collector_instances")
	requireString(&missing, manifest.Env.InstanceID, "env.instance_id")
	requireString(&missing, manifest.Env.PollInterval, "env.poll_interval")
	requireString(&missing, manifest.Env.ClaimLeaseTTL, "env.claim_lease_ttl")
	requireString(&missing, manifest.Env.HeartbeatInterval, "env.heartbeat_interval")
	requireString(&missing, manifest.Env.OwnerID, "env.owner_id")
	requireString(&missing, manifest.Env.OwnerIDConstName, "env.owner_id_const_name")
	requireString(&missing, manifest.Source.ImportPath, "source.import_path")
	requireString(&missing, manifest.Source.PackageName, "source.package_name")
	requireString(&missing, manifest.Source.ConfigType, "source.config_type")
	requireString(&missing, manifest.Source.Constructor, "source.constructor")
	requireString(&missing, manifest.Source.ConfigLoader, "source.config_loader")
	requireString(&missing, manifest.Source.ConfigAttacher, "source.config_attacher")
	requireString(&missing, manifest.Source.RuntimeConfigType, "source.runtime_config_type")
	if len(manifest.TargetIdentityFields) == 0 {
		missing = append(missing, "target_identity_fields")
	}
	if len(manifest.TargetAuthFields) == 0 {
		missing = append(missing, "target_auth_fields")
	}
	if len(missing) > 0 {
		return fmt.Errorf("collector entrypoint manifest missing or invalid fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

func requireString(missing *[]string, value string, name string) {
	if strings.TrimSpace(value) == "" {
		*missing = append(*missing, name)
	}
}

func mustGenerate(source string, manifest Manifest) []byte {
	tmpl := template.Must(template.New("entrypoint").Parse(source))
	var out bytes.Buffer
	if err := tmpl.Execute(&out, manifest); err != nil {
		panic(err)
	}
	return out.Bytes()
}

const mainTemplate = managedHeader + `package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const runtimeName = "{{.RuntimeName}}"
type launchMode string

const (
	launchModeCassette    launchMode = "cassette"
	launchModeClaimedLive launchMode = "claimed-live"
)
// launchOptions holds the parsed collector launch inputs.
type launchOptions struct {
	mode         launchMode
	cassetteFile string
}

// parseArgs parses the claimed-live or credential-free cassette launch mode.
func parseArgs(args []string) (launchOptions, error) {
	flags := flag.NewFlagSet(runtimeName, flag.ContinueOnError)
	mode := flags.String("mode", string(launchModeClaimedLive), "collector mode: claimed-live or cassette")
	cassetteFile := flags.String("cassette-file", "", "path to a cassette JSON file (cassette mode only)")
	if err := flags.Parse(args); err != nil {
		return launchOptions{}, err
	}
	selectedMode := launchMode(strings.TrimSpace(*mode))
	if selectedMode == "" {
		selectedMode = launchModeClaimedLive
	}
	switch selectedMode {
	case launchModeClaimedLive:
	case launchModeCassette:
		if strings.TrimSpace(*cassetteFile) == "" {
			return launchOptions{}, fmt.Errorf("-cassette-file is required in cassette mode")
		}
	default:
		return launchOptions{}, fmt.Errorf("unsupported -mode %q", selectedMode)
	}
	return launchOptions{mode: selectedMode, cassetteFile: strings.TrimSpace(*cassetteFile)}, nil
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "{{.BinaryName}}"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	bootstrap, err := telemetry.NewBootstrap(runtimeName)
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		fallback.Error("collector bootstrap failed", "event_name", "runtime.startup.failed", "error", err)
		os.Exit(1)
	}
	logger := telemetry.NewLogger(bootstrap, runtimeName, runtimeName)

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		logger.Error(runtimeName+" argument parsing failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
	if err := run(context.Background(), opts); err != nil {
		logger.Error("{{.RuntimeName}} failed", telemetry.EventAttr("runtime.startup.failed"), "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, opts launchOptions) error {
	bootstrap, err := telemetry.NewBootstrap(runtimeName)
	if err != nil {
		return fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(parent, bootstrap)
	if err != nil {
		return fmt.Errorf("telemetry providers: %w", err)
	}
	defer func() {
		_ = providers.Shutdown(context.Background())
	}()

	logger := telemetry.NewLogger(bootstrap, runtimeName, runtimeName)
	tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)
	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}
	pprofSrv, err := runtimecfg.NewPprofServer(os.Getenv)
	if err != nil {
		return fmt.Errorf("pprof server: %w", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Start(parent); err != nil {
			return fmt.Errorf("pprof server start: %w", err)
		}
		logger.Info("pprof server listening", "addr", pprofSrv.Addr())
		defer func() {
			_ = pprofSrv.Stop(context.Background())
		}()
	}

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	storeDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      tracer,
		Instruments: instruments,
		StoreName:   "{{.StoreName}}",
	}
	var runner app.Runner
	switch opts.mode {
	case launchModeCassette:
		runner, err = buildCassetteService(storeDB, opts.cassetteFile, tracer, instruments, logger)
	default:
		runner, err = buildClaimedService(storeDB, os.Getenv, tracer, instruments, logger)
	}
	if err != nil {
		return err
	}
	service, err := app.NewHostedWithStatusServer(
		runtimeName,
		runner,
		postgres.NewInstrumentedStatusStore(postgres.SQLQueryer{DB: db}, instruments),
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return service.Run(ctx)
}
`

const serviceTemplate = managedHeader + `package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"{{.Source.ImportPath}}"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	{{- if .MaxAttemptsExpr }}
	"github.com/eshu-hq/eshu/go/internal/workflow"
	{{- end }}
)

var fallbackClaimSequence uint64

// buildCassetteService wires a credential-free cassette source onto the shared collector commit boundary.
func buildCassetteService(
	database postgres.ExecQueryer,
	cassettePath string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return collector.Service{}, fmt.Errorf("load cassette: %w", err)
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source:       src,
		Committer:    committer,
		PollInterval: 24 * time.Hour,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}

func buildClaimedService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	{{.Source.ConfigAttacher}}(&config.Source, tracer, instruments)
	source, err := {{.Source.Constructor}}(config.Source)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       {{.CollectorKindExpr}},
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		{{- if .MaxAttemptsExpr }}
		MaxAttempts:         {{.MaxAttemptsExpr}},
		{{- end }}
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "{{.ClaimIDPrefix}}-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("{{.ClaimIDPrefix}}-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

func {{.Source.ConfigAttacher}}(config *{{.Source.ConfigType}}, tracer trace.Tracer, instruments *telemetry.Instruments) {
	config.Tracer = tracer
	config.Instruments = instruments
}
`

const configTemplate = managedHeader + `package main

import (
	"fmt"
	"strings"
	"time"

	"{{.Source.ImportPath}}"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "{{.Env.InstanceID}}"
	envPollInterval        = "{{.Env.PollInterval}}"
	envClaimLeaseTTL       = "{{.Env.ClaimLeaseTTL}}"
	envHeartbeatInterval   = "{{.Env.HeartbeatInterval}}"
	{{.Env.OwnerIDConstName}} = "{{.Env.OwnerID}}"
	envCollectorInstances  = "{{.Env.CollectorInstances}}"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Source            {{.Source.ConfigType}}
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := select{{.GoName}}Instance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validate{{.GoName}}Instance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	sourceConfig, err := {{.Source.ConfigLoader}}(instance, getenv)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, time.Second)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return claimedRuntimeConfig{}, fmt.Errorf("{{.CollectorLabel}} heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Source:            sourceConfig,
	}, nil
}

func select{{.GoName}}Instance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != {{.CollectorKindExpr}} {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("{{.CollectorLabel}} instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no {{.CollectorLabel}} instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple {{.CollectorLabel}} instances configured; set %s", envCollectorInstanceID)
	}
}

func validate{{.GoName}}Instance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("{{.CollectorLabel}} instance: %w", err)
	}
	if instance.CollectorKind != {{.CollectorKindExpr}} {
		return fmt.Errorf("{{.CollectorLabel}} requires collector_kind %q", {{.CollectorKindExpr}})
	}
	if !instance.Enabled {
		return fmt.Errorf("{{.CollectorLabel}} requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("{{.CollectorLabel}} requires claim-enabled collector instance")
	}
	return nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{ {{.Env.OwnerIDConstName}}, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "{{.RuntimeName}}"
}
`
