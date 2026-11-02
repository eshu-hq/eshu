// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// EnvClaimPlaneURL names the variable that points a collector at the
// eshu-workflow claim plane. Unset or blank keeps the core Postgres store.
const EnvClaimPlaneURL = "ESHU_WORKFLOW_CLAIM_PLANE_URL"

// RequestTimeout bounds one claim-plane HTTP round trip. It is half the
// default 20 s heartbeat interval and a sixth of the default 60 s lease, so a
// timed-out heartbeat still leaves the lease alive for the next tick.
const RequestTimeout = 10 * time.Second

// Backend names the claim store a collector selected. The values are the
// backend_kind label on eshu_dp_workflow_control_store_selections_total.
type Backend string

const (
	// BackendPostgres is the in-tree core Postgres workflow control store.
	BackendPostgres Backend = "postgres"
	// BackendClaimPlane is the eshu-workflow claim plane over workflow.v1.
	BackendClaimPlane Backend = "claim_plane"
)

// ErrClaimPlaneUnsupported marks a collector that cannot run against the
// claim plane because it reads workflow rows only core Postgres holds.
var ErrClaimPlaneUnsupported = errors.New("collector cannot use the workflow claim plane")

// Log keys for the startup selection record.
const (
	logKeyBackend  = "workflow_control_store"
	logKeyEndpoint = "claim_plane_endpoint"
	logKeyService  = "service"
	eventSelected  = "collector.workflow_control_store.selected"
)

// Options configures New.
type Options struct {
	// Service is the collector runtime name, used in logs and errors.
	Service string
	// Getenv reads the process environment. Required.
	Getenv func(string) string
	// Postgres is the core store the caller built with
	// postgres.NewWorkflowControlStore. Required; returned unchanged when
	// the claim-plane variable is unset.
	Postgres collector.ClaimControlStore
	// CoreOnlyReason, when non-empty, marks a collector that reads workflow
	// rows only core Postgres holds. Setting the variable for such a
	// collector is a startup error that carries this reason.
	CoreOnlyReason string
	// HTTPClient overrides the bounded default client. Tests use it; a
	// production caller leaves it nil.
	HTTPClient *http.Client
	// Logger receives the startup selection record. Optional.
	Logger *slog.Logger
	// Instruments receives the selection counter and the claim-plane request
	// metrics. Optional.
	Instruments *telemetry.Instruments
}

// Selection is the store New chose.
type Selection struct {
	// Store is the claim store to set on collector.ClaimedService.
	Store collector.ClaimControlStore
	// Backend names the selected store.
	Backend Backend
	// Endpoint is the claim-plane scheme and host; empty for Postgres.
	Endpoint string
}

// New selects the collector claim store from EnvClaimPlaneURL. It returns
// opts.Postgres unchanged when the variable is unset or blank, a bounded
// workflow.ClaimPlaneClient when it is a valid base URL, and an error naming
// the variable otherwise. It never falls back from the claim plane to
// Postgres.
func New(ctx context.Context, opts Options) (Selection, error) {
	if opts.Getenv == nil {
		return Selection{}, errors.New("claim store selection requires an environment reader")
	}
	if opts.Postgres == nil {
		return Selection{}, errors.New("claim store selection requires the core Postgres store")
	}
	raw := strings.TrimSpace(opts.Getenv(EnvClaimPlaneURL))
	if raw == "" {
		selection := Selection{Store: opts.Postgres, Backend: BackendPostgres}
		recordSelection(ctx, opts, selection)
		return selection, nil
	}
	if reason := strings.TrimSpace(opts.CoreOnlyReason); reason != "" {
		return Selection{}, fmt.Errorf("%w: %s is set but %s %s; unset it for this collector",
			ErrClaimPlaneUnsupported, EnvClaimPlaneURL, opts.Service, reason)
	}
	endpoint, err := parseClaimPlaneURL(raw)
	if err != nil {
		return Selection{}, fmt.Errorf("%s: %w", opts.Service, err)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: RequestTimeout}
	}
	client := workflow.NewClaimPlaneClient(endpoint.String(), httpClient)
	client.Observe = requestObserver(opts.Instruments)
	selection := Selection{Store: client, Backend: BackendClaimPlane, Endpoint: endpoint.String()}
	recordSelection(ctx, opts, selection)
	return selection, nil
}

// recordSelection emits the startup log record and selection counter.
func recordSelection(ctx context.Context, opts Options, selection Selection) {
	if opts.Instruments != nil && opts.Instruments.WorkflowControlStoreSelections != nil {
		opts.Instruments.WorkflowControlStoreSelections.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrBackendKind(string(selection.Backend))))
	}
	if opts.Logger == nil {
		return
	}
	attrs := []any{
		telemetry.EventAttr(eventSelected),
		slog.String(logKeyService, opts.Service),
		slog.String(logKeyBackend, string(selection.Backend)),
	}
	if selection.Endpoint != "" {
		attrs = append(attrs, slog.String(logKeyEndpoint, selection.Endpoint))
	}
	opts.Logger.InfoContext(ctx, "workflow claim store selected", attrs...)
}

// requestObserver binds the claim-plane client hook to the request metrics.
// It returns nil when no instruments are wired, so the client skips the call.
func requestObserver(instruments *telemetry.Instruments) workflow.ClaimPlaneObserver {
	if instruments == nil || instruments.WorkflowClaimPlaneRequests == nil ||
		instruments.WorkflowClaimPlaneRequestDuration == nil {
		return nil
	}
	return func(ctx context.Context, operation string, outcome workflow.ClaimPlaneOutcome, elapsed time.Duration) {
		attrs := metric.WithAttributes(telemetry.AttrOperation(operation), telemetry.AttrOutcome(string(outcome)))
		instruments.WorkflowClaimPlaneRequests.Add(ctx, 1, attrs)
		instruments.WorkflowClaimPlaneRequestDuration.Record(ctx, elapsed.Seconds(), attrs)
	}
}
