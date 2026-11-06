// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	// maxStdioRequestBytes caps one host claim envelope on stdin. The host
	// request is a bounded claim document; anything past the cap fails
	// closed instead of allocating unbounded input.
	maxStdioRequestBytes = 4 << 20

	// sdkStdioFlag selects the satellite claim adapter: one host claim on
	// stdin drains to one SDK result on stdout, instead of the default
	// one-shot Postgres drain.
	sdkStdioFlag = "--sdk-stdio"
	// grantDeniedFailureClass is the bounded failure class a terminal
	// denial ack carries. The host records it on the claim, so a
	// deterministic denial terminal-fails instead of burning the attempt
	// budget on retries that can never succeed.
	grantDeniedFailureClass = "grant_denied"
	// componentHomeEnv is the registry home the activation reconcile
	// populates with live producer grants before install. It is owned by
	// the component registry contract; this caller only reads it.
	componentHomeEnv = "ESHU_COMPONENT_HOME"
)

// isSDKStdio reports whether the invocation selects the satellite claim
// adapter.
func isSDKStdio(args []string) bool {
	for _, arg := range args {
		if arg == sdkStdioFlag {
			return true
		}
	}
	return false
}

// runSDKStdio serves one host claim with the production source opener: the
// pinned whisper.cpp backend is verified at drain start (binary and model
// digests plus the engine version) and the activation artifact directory is
// drained through the existing source. The satellite opens no database: the
// drained envelopes commit into the SDK result for the host's durable
// commit after this adapter's grant checks pass.
func runSDKStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return serveStdioClaim(ctx, stdin, stdout, os.Getenv, openProductionSource)
}

// runSDKStdioCommand serves one host claim on stdio. Only the bare
// --sdk-stdio flag is admitted: any other argument is a miswired adapter
// invocation and fails closed instead of serving under surprising flags.
func runSDKStdioCommand(ctx context.Context, args []string) error {
	for _, arg := range args {
		if arg != sdkStdioFlag {
			return fmt.Errorf("collector --sdk-stdio takes no other arguments, got %q", arg)
		}
	}
	return runSDKStdio(ctx, os.Stdin, os.Stdout)
}

// openProductionSource verifies the pinned backend and opens the activation
// artifact directory, mirroring the one-shot buildDrain wiring without its
// database handle.
func openProductionSource(cfg Config, logger *slog.Logger) (collector.Source, error) {
	engine, err := buildEngine(cfg.Whisper, nil, nil, logger)
	if err != nil {
		return nil, err
	}
	return newDirSource(cfg.ArtifactDir, sourceOptions{
		engine:        engine,
		sourceSystem:  cfg.SourceSystem,
		scopePrefix:   cfg.ScopePrefix,
		maxInputBytes: cfg.MaxInputBytes,
		maxArtifacts:  cfg.MaxArtifacts,
	}, logger)
}

// openStdioSource builds the claim's drain source from validated startup
// configuration. Production verifies the pinned whisper.cpp backend (binary
// and model digests plus the engine version) and opens the activation
// artifact directory; tests inject the stub transcript engine over the same
// source seam.
type openStdioSource func(Config, *slog.Logger) (collector.Source, error)

// serveStdioClaim serves one host claim from stdin and writes one SDK
// result to stdout. It runs the existing startup verification, drains the
// activation artifact dir through the untouched drainOnce, and commits the
// drained envelopes into the SDK result only when the activation's live
// producer grants authorize every emitted kind. A deterministic grant denial
// acks a terminal result so the host terminal-fails the claim; any other
// failure is a process error so the host can retry it.
func serveStdioClaim(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
	getenv func(string) string,
	openSource openStdioSource,
) error {
	request, err := decodeStdioRequest(stdin)
	if err != nil {
		return err
	}
	cfg, err := loadMediaImportConfig(getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("media import satellite is disabled without %s=true", envEnabled)
	}
	logger := slog.Default()
	manifest, err := loadMediaSDKManifest()
	if err != nil {
		return err
	}
	contract, err := sdkContractForManifest(manifest)
	if err != nil {
		return err
	}
	validator := sdkcollector.NewValidator(contract)
	grantHome := ""
	if getenv != nil {
		grantHome = strings.TrimSpace(getenv(componentHomeEnv))
	}
	grants := liveProducerGrants(grantHome)
	if err := manifest.ValidateWithGrants(grants); err != nil {
		return denyStdioClaim(stdout, validator, request, logger, fmt.Errorf("media-import activation denied: %w", err))
	}
	src, err := openSource(cfg, logger)
	if err != nil {
		return err
	}
	capture := &captureCommitter{}
	generations, _, err := drainOnce(ctx, src, capture, logger)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, envelopes := range capture.facts {
		for _, envelope := range envelopes {
			allowed, _ := component.EvaluateEmission(
				grants,
				manifest.Metadata.ID,
				manifest.Metadata.Version,
				envelope.FactKind,
				envelope.SchemaVersion,
				manifest.Spec.CollectorKinds,
				now,
			)
			if !allowed {
				return denyStdioClaim(stdout, validator, request, logger, fmt.Errorf(
					"fact kind %q is core-owned by Eshu and has no live producer grant for %q version %q",
					envelope.FactKind, manifest.Metadata.ID, manifest.Metadata.Version,
				))
			}
		}
	}
	result, err := ackResult(request, capture, generations)
	if err != nil {
		return err
	}
	if _, err := validator.ValidateResult(result); err != nil {
		return fmt.Errorf("validate claim result against SDK contract: %w", err)
	}
	return encodeStdioResult(stdout, result)
}

// denyStdioClaim acks a deterministic grant denial as a terminal result with
// zero facts. The cause is logged for the operator; the ack itself carries
// only the bounded failure class, so no grant material or payload ever
// reaches the wire.
func denyStdioClaim(stdout io.Writer, validator sdkcollector.Validator, request extensionhost.Request, logger *slog.Logger, cause error) error {
	if logger != nil {
		logger.Warn("sdk-stdio grant denied", "error", cause)
	}
	result := sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultTerminal,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: time.Now().UTC(),
		},
		Statuses: []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: grantDeniedFailureClass}},
	}
	if _, err := validator.ValidateResult(result); err != nil {
		return fmt.Errorf("sdk-stdio denial fails SDK validation: %w", err)
	}
	return encodeStdioResult(stdout, result)
}

// loadMediaSDKManifest decodes the embedded component manifest without grant
// context: grant admission runs separately so a denial acks terminal
// instead of failing the process.
func loadMediaSDKManifest() (component.Manifest, error) {
	var manifest component.Manifest
	if err := yaml.Unmarshal(embeddedManifest, &manifest); err != nil {
		return component.Manifest{}, fmt.Errorf("sdk-stdio decodes the embedded manifest: %w", err)
	}
	return manifest, nil
}

// sdkContractForManifest builds the SDK validation contract from the
// embedded manifest, mirroring the host's admission contract: the protocol
// the manifest declares and exactly the fact families it emits.
func sdkContractForManifest(manifest component.Manifest) (sdkcollector.Contract, error) {
	contract := sdkcollector.Contract{
		ProtocolVersion: manifest.Spec.Runtime.SDKProtocol,
		Facts:           make([]sdkcollector.FactDeclaration, 0, len(manifest.Spec.EmittedFacts)),
	}
	for _, family := range manifest.Spec.EmittedFacts {
		declaration := sdkcollector.FactDeclaration{
			Kind:             family.Kind,
			SchemaVersions:   append([]string(nil), family.SchemaVersions...),
			SourceConfidence: make([]sdkcollector.SourceConfidence, 0, len(family.SourceConfidence)),
		}
		for _, confidence := range family.SourceConfidence {
			declaration.SourceConfidence = append(declaration.SourceConfidence, sdkcollector.SourceConfidence(confidence))
		}
		contract.Facts = append(contract.Facts, declaration)
	}
	if len(contract.Facts) == 0 {
		return sdkcollector.Contract{}, fmt.Errorf("sdk-stdio manifest %q declares no emitted fact families", manifest.Metadata.ID)
	}
	return contract, nil
}

// liveProducerGrants reads the activation's producer grants from the
// component registry home. An empty home or an unreadable set yields no
// grants, which denies core-owned emission fail-closed exactly like the
// host's emission recheck.
func liveProducerGrants(home string) []component.ProducerGrant {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	grants, err := component.NewRegistry(home).ProducerGrants()
	if err != nil {
		return nil
	}
	return grants
}

// encodeStdioResult writes one indented SDK result: the ack the host
// decodes and commits.
func encodeStdioResult(stdout io.Writer, result sdkcollector.Result) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode claim result: %w", err)
	}
	return nil
}

// decodeStdioRequest reads exactly one host claim envelope. A foreign
// protocol version or an unnamed generation fails closed: the adapter can
// neither drain nor ack a claim it cannot name on the pinned contract.
func decodeStdioRequest(stdin io.Reader) (extensionhost.Request, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdioRequestBytes+1))
	if err != nil {
		return extensionhost.Request{}, fmt.Errorf("read host request: %w", err)
	}
	if len(raw) > maxStdioRequestBytes {
		return extensionhost.Request{}, fmt.Errorf("host request exceeds %d bytes", maxStdioRequestBytes)
	}
	var request extensionhost.Request
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&request); err != nil {
		return extensionhost.Request{}, fmt.Errorf("decode host request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return extensionhost.Request{}, fmt.Errorf("decode host request: trailing JSON value")
		}
		return extensionhost.Request{}, fmt.Errorf("decode host request trailer: %w", err)
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdkcollector.ProtocolVersionV1Alpha1 {
		return extensionhost.Request{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	if strings.TrimSpace(request.Claim.GenerationID) == "" {
		return extensionhost.Request{}, fmt.Errorf("host claim names no generation")
	}
	return request, nil
}

// captureCommitter is the satellite's commit sink: drainOnce commits every
// drained generation into memory, and serveStdioClaim projects the captured
// envelopes onto the SDK wire shape for the host's grant-checked commit.
// It implements collector.Committer.
type captureCommitter struct {
	scopes []scope.IngestionScope
	gens   []scope.ScopeGeneration
	facts  [][]facts.Envelope
}

// compile-time proof that the capture sink satisfies the shared commit
// boundary drainOnce commits through.
var _ collector.Committer = (*captureCommitter)(nil)

// CommitScopeGeneration drains the fact stream into memory without a store.
func (c *captureCommitter) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	ch <-chan facts.Envelope,
) error {
	var envelopes []facts.Envelope
	for envelope := range ch {
		envelopes = append(envelopes, envelope)
	}
	c.scopes = append(c.scopes, scopeValue)
	c.gens = append(c.gens, generation)
	c.facts = append(c.facts, envelopes)
	return nil
}

// ackResult projects every captured envelope onto the SDK wire shape and
// builds the claim ack: the claim echoed verbatim, the claimed generation,
// the drained facts in drain order, and one complete status row counting
// them. An empty drain acks unchanged with zero facts.
func ackResult(
	request extensionhost.Request,
	capture *captureCommitter,
	generations int,
) (sdkcollector.Result, error) {
	var wire []sdkcollector.Fact
	for _, envelopes := range capture.facts {
		for _, envelope := range envelopes {
			fact, err := factenvelope.SDKFactFromInternal(envelope)
			if err != nil {
				return sdkcollector.Result{}, fmt.Errorf("project envelope onto SDK shape: %w", err)
			}
			// Name the claim the host commits under: the host
			// persists satellite results under the claimed work
			// item's source system, scope, and generation, and the
			// SDK contract requires the refs to match. The drain's
			// content-derived identity survives in the stable
			// key and payload, so re-runs stay idempotent.
			fact.SourceRef.SourceSystem = request.Claim.SourceSystem
			fact.SourceRef.ScopeID = request.Claim.Scope.ID
			fact.SourceRef.GenerationID = request.Claim.GenerationID
			wire = append(wire, fact)
		}
	}
	state := sdkcollector.ResultComplete
	if generations == 0 {
		state = sdkcollector.ResultUnchanged
	}
	return sdkcollector.Result{
		ProtocolVersion: request.ProtocolVersion,
		State:           state,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: time.Now().UTC(),
		},
		Facts: wire,
		Statuses: []sdkcollector.Status{{
			Class:     sdkcollector.StatusComplete,
			FactCount: len(wire),
		}},
	}, nil
}
