// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// DefaultCommitHandlerTimeout bounds one core commit from envelope read to
// COMMIT. It stays under the satellite CommitForwardTimeout (120 s) with
// margin: the pin attempt inside the boundary is itself bounded at 10 s
// (DefaultClaimFencePinAttemptTimeout), so an overrun here answers 500
// through the proxy while the proxy client is still waiting, and the proxy
// retries instead of completing. A slower core would die to the proxy
// timeout with an ambiguous outcome on every near-cap commit.
const DefaultCommitHandlerTimeout = 60 * time.Second

// CommitHandlerConfig carries the core-side commit bundle: the satellite
// component's manifest (contract, payload schemas, allowed kinds), the live
// grant reader, the claim-plane control store the epoch lives on, and the
// claim-fenced committer that persists it. It mirrors the service wiring
// that pairs a claim-plane client with IngestionStore{ClaimPlane}.
type CommitHandlerConfig struct {
	// Manifest is the satellite component manifest. Its emitted facts seed
	// the result validator and payload schemas; its collector kinds gate
	// the per-kind scope builder (unknown kinds fail closed).
	Manifest component.Manifest
	// Grants is the activation-time grant snapshot. LiveGrants, when set,
	// re-reads the registry on every commit so a revoked grant fails the
	// next result closed; nil LiveGrants reuses the snapshot.
	Grants []component.ProducerGrant
	// LiveGrants re-reads producer grants per commit. Optional.
	LiveGrants func() ([]component.ProducerGrant, error)
	// ScopeKind is the scope kind committed generations carry.
	ScopeKind scope.ScopeKind
	// SourceSystem is the source system this handler serves. The committed
	// scope identity is built from it, never from the producer-controlled
	// result echo: an echo naming any other source system fails closed in
	// the identity match, so a fencing-token holder cannot overwrite
	// canonical scope rows that belong to another system.
	SourceSystem string
	// CollectorInstanceID binds the result echo to this producer.
	CollectorInstanceID string
	// ControlStore is the claim store holding the fenced epoch. The fence
	// probe (HeartbeatClaim) runs on it before any byte is committed.
	ControlStore collector.ClaimControlStore
	// Committer persists the generation. It must implement
	// collector.ClaimedCommitter with the claim-plane fence: the
	// commit-time pin that binds scope identity runs only there, so an
	// unfenced committer is refused at construction.
	Committer collector.Committer
	// Clock stamps ingestion time. Nil uses time.Now.
	Clock func() time.Time
	// CommitTimeout bounds one commit. Non-positive uses the default, which
	// stays under the 120 s proxy bound.
	CommitTimeout time.Duration
}

// CommitHandler serves the core side of POST
// /workflow.v1/CommitClaimFacts: the endpoint the satellite proxy forwards
// to byte-verbatim. It enforces the shared body cap without trusting the
// proxy, runs the full admission stack the in-tree path runs
// (validator.ValidateResult, bounded statuses, the echo-vs-planned match,
// payload schemas, producer-grant coverage), then commits through
// collector.CommitClaimFacts and answers {"ok":true} only after COMMIT.
type CommitHandler struct {
	componentID    string
	instanceID     string
	sourceSystem   string
	scopeKind      scope.ScopeKind
	collectorKinds map[string]struct{}
	validator      sdkcollector.Validator
	payloadSchemas map[string]json.RawMessage
	manifest       component.Manifest
	grants         []component.ProducerGrant
	liveGrants     func() ([]component.ProducerGrant, error)
	store          collector.ClaimControlStore
	committer      collector.Committer
	clock          func() time.Time
	commitTimeout  time.Duration
}

// NewCommitHandler validates the commit bundle fail-closed: no store, no
// unfenced committer, no manifest kinds, no scope kind, no source system,
// and no instance identity ever build a handler that would admit a result
// it cannot fence or attribute. The fenced committer is unconditional:
// the scope-binding pin runs only on the plane path, so every allowed
// production wiring (IngestionStore with ClaimPlane plus Pinner) pins
// before COMMIT; test and stub wirings record without pinning.
func NewCommitHandler(cfg CommitHandlerConfig) (*CommitHandler, error) {
	if cfg.ControlStore == nil {
		return nil, errors.New("commit handler: claim control store is required")
	}
	if cfg.Committer == nil {
		return nil, errors.New("commit handler: collector committer is required")
	}
	if strings.TrimSpace(string(cfg.ScopeKind)) == "" {
		return nil, errors.New("commit handler: scope kind is required")
	}
	if strings.TrimSpace(cfg.CollectorInstanceID) == "" {
		return nil, errors.New("commit handler: collector instance id is required")
	}
	if strings.TrimSpace(cfg.SourceSystem) == "" {
		return nil, errors.New("commit handler: source system is required")
	}
	componentID := strings.TrimSpace(cfg.Manifest.Metadata.ID)
	if componentID == "" {
		return nil, errors.New("commit handler: component manifest id is required")
	}
	// Fail fast without a claim-plane-fenced committer: the commit-time
	// pin that binds scope, source run, and generation to the live epoch
	// only runs on the plane path, so an unfenced committer would admit
	// commits whose scope identity nothing verifies. The store half stays
	// open (any ClaimControlStore probes the epoch), but every allowed
	// production wiring (IngestionStore with ClaimPlane plus Pinner) pins
	// before COMMIT; test and stub wirings record without pinning.
	if !collector.CommitterFencesClaimPlane(cfg.Committer) {
		return nil, errors.New("commit handler: claim-plane-fenced committer is required: the commit-time pin that binds scope identity runs only on the plane path")
	}
	contract, err := sdkContract(cfg.Manifest)
	if err != nil {
		return nil, fmt.Errorf("commit handler: %w", err)
	}
	kinds := make(map[string]struct{}, len(cfg.Manifest.Spec.CollectorKinds))
	for _, kind := range cfg.Manifest.Spec.CollectorKinds {
		if trimmed := strings.TrimSpace(kind); trimmed != "" {
			kinds[trimmed] = struct{}{}
		}
	}
	if len(kinds) == 0 {
		return nil, errors.New("commit handler: component manifest declares no collector kinds")
	}
	grants := append([]component.ProducerGrant(nil), cfg.Grants...)
	liveGrants := cfg.LiveGrants
	if liveGrants == nil {
		liveGrants = func() ([]component.ProducerGrant, error) { return grants, nil }
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	timeout := cfg.CommitTimeout
	if timeout <= 0 {
		timeout = DefaultCommitHandlerTimeout
	}
	return &CommitHandler{
		componentID:    componentID,
		instanceID:     strings.TrimSpace(cfg.CollectorInstanceID),
		sourceSystem:   strings.TrimSpace(cfg.SourceSystem),
		scopeKind:      cfg.ScopeKind,
		collectorKinds: kinds,
		validator:      sdkcollector.NewValidator(contract),
		payloadSchemas: payloadSchemasForManifest(cfg.Manifest),
		manifest:       cfg.Manifest,
		grants:         grants,
		liveGrants:     liveGrants,
		store:          cfg.ControlStore,
		committer:      cfg.Committer,
		clock:          clock,
		commitTimeout:  timeout,
	}, nil
}

// CommitClaimFactsRoutePath returns the protocol mount from the shared route
// table: the handler serves exactly the operation the released satellite
// proxy forwards to, never a local literal that can drift.
func CommitClaimFactsRoutePath() string {
	route, ok := workflowv1.RouteFor(workflowv1.OpCommitClaimFacts)
	if !ok {
		return "/workflow.v1/CommitClaimFacts"
	}
	return route.Path
}

// NewCommitClaimFactsMux mounts one handler on the protocol route. The
// method comes from the route table, so a non-POST forward answers 405
// before the handler ever runs.
func NewCommitClaimFactsMux(handler *CommitHandler) *http.ServeMux {
	mux := http.NewServeMux()
	route, ok := workflowv1.RouteFor(workflowv1.OpCommitClaimFacts)
	pattern := http.MethodPost + " " + CommitClaimFactsRoutePath()
	if ok {
		pattern = string(route.Method) + " " + route.Path
	}
	mux.Handle(pattern, handler)
	return mux
}

// ServeHTTP implements the core commit endpoint. The order is fixed: version
// gate, bounded body read, strict envelope decode, envelope contract, strict
// result decode, full admission, fenced commit, ack-only-after-COMMIT. Every
// refusal carries the workflow.v1 error envelope the proxy copies back
// verbatim, so fence and lease refusals keep their 409 shape end to end.
func (h *CommitHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	const operation = "CommitClaimFacts"
	if request.Method != http.MethodPost {
		writeCommitError(writer, http.StatusMethodNotAllowed, workflowv1.CodeInvalid, "commit requires POST")
		return
	}
	if err := workflowv1.CheckVersion(request.Header.Get(workflowv1.VersionHeader)); err != nil {
		if proto, ok := asProtocolError(err); ok {
			writeCommitError(writer, proto.HTTPStatus(), proto.Code, proto.Message)
			return
		}
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeVersionMismatch, "unsupported protocol version")
		return
	}
	raw := readCommitBody(writer, request)
	if raw == nil {
		return
	}
	var wire workflowv1.CommitClaimFactsRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid, fmt.Sprintf("decode %s request: %v", operation, err))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid, fmt.Sprintf("decode %s request: trailing data after JSON document", operation))
		return
	}
	if err := wire.Validate(); err != nil {
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid, fmt.Sprintf("invalid %s request: %v", operation, err))
		return
	}
	mutation := wireMutationToMutation(wire)
	result, err := h.admitResult(mutation, wire.ResultJSON)
	if err != nil {
		if proto, ok := asProtocolError(err); ok {
			writeCommitError(writer, proto.HTTPStatus(), proto.Code, proto.Message)
			return
		}
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid, err.Error())
		return
	}
	scopeValue, generation, err := h.buildTarget(result)
	if err != nil {
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid, err.Error())
		return
	}
	ctx, cancel := h.commitContext(request.Context())
	defer cancel()
	req := collector.CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          scopeValue,
		Generation:     generation,
		ComponentID:    h.componentID,
		IdempotencyKey: strings.TrimSpace(wire.IdempotencyKey),
		ResultJSON:     []byte(wire.ResultJSON),
	}
	if err := collector.CommitClaimFacts(ctx, h.store, h.committer, req); err != nil {
		code, status := classifyCommitError(ctx, err)
		writeCommitError(writer, status, code, commitErrorMessage(err))
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
}
