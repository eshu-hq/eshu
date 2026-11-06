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
	// sdkStdioMaxRequestBytes caps one host claim request, mirroring the
	// synthetic window emitter: the host's request is a bounded claim
	// envelope and anything past the cap fails closed.
	sdkStdioMaxRequestBytes = 4 << 20
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

// sdkStdioDeps carries the stdio adapter's seams. Production wires the
// installed manifest, the activation registry home, and the pinned engine;
// tests inject a stub extractor, a temp manifest, and a temp grant home so
// no test needs tesseract, Postgres, or a live backend.
type sdkStdioDeps struct {
	getenv       func(string) string
	manifestPath string
	grantHome    string
	newExtractor func(Config) (extractor, error)
	logger       *slog.Logger
}

// captureCommitter holds one drain in memory instead of committing it to
// Postgres: the satellite child has no database handle, so the drain's
// envelopes are projected onto the SDK wire and acked for the host to
// commit through its own grant-checked path.
type captureCommitter struct {
	envelopes []facts.Envelope
}

// compile-time proof that the capture stays on the shared commit boundary.
var _ collector.Committer = (*captureCommitter)(nil)

// CommitScopeGeneration drains one generation's fact channel into memory.
func (c *captureCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	channel <-chan facts.Envelope,
) error {
	for envelope := range channel {
		c.envelopes = append(c.envelopes, envelope)
	}
	return nil
}

// serveSDKStdio serves one host claim from stdin and writes one SDK result
// to stdout. It runs the existing startup verification (environment plus
// pinned engine pins), drains the activation artifact dir through the
// untouched drainOnce, and commits the drained envelopes into the SDK
// result only when the activation's live producer grants authorize every
// emitted kind. A deterministic grant denial acks a terminal result so the
// host terminal-fails the claim; any other failure is a process error so
// the host can retry it.
func serveSDKStdio(ctx context.Context, stdin io.Reader, stdout io.Writer, deps sdkStdioDeps) error {
	getenv := deps.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	logger := deps.logger
	if logger == nil {
		logger = sdkStdioDiscardLogger()
	}
	manifestPath := deps.manifestPath
	if manifestPath == "" {
		manifestPath = installedManifestPath
	}
	newExtractor := deps.newExtractor
	if newExtractor == nil {
		newExtractor = defaultSDKStdioExtractor
	}

	request, err := decodeSDKStdioRequest(stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdkcollector.ProtocolVersionV1Alpha1 {
		return fmt.Errorf("sdk-stdio protocol_version %q is unsupported; this caller speaks %q", request.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1)
	}
	cfg, err := loadImportConfig(getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("sdk-stdio claim refused: the document import collector is disabled (set %s=true with a QA tier, an artifact dir and pinned engine)", envEnabled)
	}
	manifest, err := loadSDKStdioManifest(manifestPath)
	if err != nil {
		return err
	}
	contract, err := sdkContractForManifest(manifest)
	if err != nil {
		return err
	}
	validator := sdkcollector.NewValidator(contract)
	grantHome := deps.grantHome
	if grantHome == "" {
		grantHome = strings.TrimSpace(getenv(componentHomeEnv))
	}
	grants := liveProducerGrants(grantHome)
	if err := manifest.ValidateWithGrants(grants); err != nil {
		return denySDKStdio(stdout, validator, request.Claim, logger, fmt.Errorf("document-import activation denied: %w", err))
	}
	engine, err := newExtractor(cfg)
	if err != nil {
		return err
	}
	src, err := newDirSource(cfg.ArtifactDir, sourceOptions{
		engine:        engine,
		sourceSystem:  cfg.SourceSystem,
		scopePrefix:   cfg.ScopePrefix,
		maxInputBytes: cfg.Tesseract.MaxInputBytes,
		maxArtifacts:  cfg.MaxArtifacts,
	}, logger)
	if err != nil {
		return err
	}
	capture := &captureCommitter{}
	if _, _, err := drainOnce(ctx, src, capture, logger); err != nil {
		return err
	}
	now := time.Now().UTC()
	emitted := make([]sdkcollector.Fact, 0, len(capture.envelopes))
	for _, envelope := range capture.envelopes {
		fact, err := factenvelope.SDKFactFromInternal(envelope)
		if err != nil {
			return fmt.Errorf("sdk-stdio projects generation %q: %w", envelope.GenerationID, err)
		}
		// The satellite speaks for the claim: the wire binds every fact
		// to the claimed scope and generation while the content-derived
		// identity (stable key and payload) travels unchanged. The host
		// re-keys to the work item again before its durable commit, so no
		// content identity is lost at commit time.
		fact.SourceRef.SourceSystem = request.Claim.SourceSystem
		fact.SourceRef.ScopeID = request.Claim.Scope.ID
		fact.SourceRef.GenerationID = request.Claim.GenerationID
		allowed, _ := component.EvaluateEmission(
			grants,
			manifest.Metadata.ID,
			manifest.Metadata.Version,
			fact.Kind,
			fact.SchemaVersion,
			manifest.Spec.CollectorKinds,
			now,
		)
		if !allowed {
			return denySDKStdio(stdout, validator, request.Claim, logger, fmt.Errorf(
				"fact kind %q is core-owned by Eshu and has no live producer grant for %q version %q",
				fact.Kind, manifest.Metadata.ID, manifest.Metadata.Version,
			))
		}
		emitted = append(emitted, fact)
	}
	result := sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: now,
		},
	}
	if len(emitted) == 0 {
		result.State = sdkcollector.ResultUnchanged
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete, FactCount: 0}}
	} else {
		result.State = sdkcollector.ResultComplete
		result.Facts = emitted
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete, FactCount: len(emitted)}}
	}
	if _, err := validator.ValidateResult(result); err != nil {
		return fmt.Errorf("sdk-stdio result fails SDK validation: %w", err)
	}
	return encodeSDKStdioResult(stdout, result)
}

// denySDKStdio acks a deterministic grant denial as a terminal result with
// zero facts. The cause is logged for the operator; the ack itself carries
// only the bounded failure class, so no grant material or payload ever
// reaches the wire.
func denySDKStdio(stdout io.Writer, validator sdkcollector.Validator, claim sdkcollector.Claim, logger *slog.Logger, cause error) error {
	if logger != nil {
		logger.Warn("sdk-stdio grant denied", "error", cause)
	}
	result := sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultTerminal,
		Claim:           claim,
		Generation: sdkcollector.Generation{
			ID:         claim.GenerationID,
			ObservedAt: time.Now().UTC(),
		},
		Statuses: []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: grantDeniedFailureClass}},
	}
	if _, err := validator.ValidateResult(result); err != nil {
		return fmt.Errorf("sdk-stdio denial fails SDK validation: %w", err)
	}
	return encodeSDKStdioResult(stdout, result)
}

// decodeSDKStdioRequest reads one bounded host request. A second JSON value
// fails closed: the host sends exactly one request per invocation.
func decodeSDKStdioRequest(stdin io.Reader) (extensionhost.Request, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, sdkStdioMaxRequestBytes+1))
	if err != nil {
		return extensionhost.Request{}, fmt.Errorf("sdk-stdio reads the host request: %w", err)
	}
	if len(raw) > sdkStdioMaxRequestBytes {
		return extensionhost.Request{}, fmt.Errorf("sdk-stdio host request exceeds %d bytes", sdkStdioMaxRequestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var request extensionhost.Request
	if err := decoder.Decode(&request); err != nil {
		return extensionhost.Request{}, fmt.Errorf("sdk-stdio decodes the host request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return extensionhost.Request{}, fmt.Errorf("sdk-stdio host request carries a trailing JSON value")
		}
		return extensionhost.Request{}, fmt.Errorf("sdk-stdio decodes the host request trailer: %w", err)
	}
	return request, nil
}

// loadSDKStdioManifest reads the installed component manifest without grant
// context: grant admission runs separately so a denial acks terminal
// instead of failing the process.
func loadSDKStdioManifest(path string) (component.Manifest, error) {
	// #nosec G304 -- path is the installed /manifest.yaml constant, never operator free-form input.
	raw, err := os.ReadFile(path)
	if err != nil {
		return component.Manifest{}, fmt.Errorf("sdk-stdio reads the installed manifest %q: %w", path, err)
	}
	var manifest component.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return component.Manifest{}, fmt.Errorf("sdk-stdio decodes the installed manifest %q: %w", path, err)
	}
	return manifest, nil
}

// sdkContractForManifest builds the SDK validation contract from the
// installed manifest, mirroring the host's admission contract: the protocol
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

// defaultSDKStdioExtractor verifies the pinned tesseract installation
// through the existing engine path with satellite telemetry: no providers
// and no database, so the child carries no exporter or store handles.
func defaultSDKStdioExtractor(cfg Config) (extractor, error) {
	return buildEngine(cfg.Tesseract, nil, nil, sdkStdioDiscardLogger())
}

// sdkStdioDiscardLogger silences routine drain lines in the satellite
// child: operator visibility for the claim comes from the host's status
// rows, and failures surface through the returned error on stderr.
func sdkStdioDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// hasSDKStdioFlag reports whether the invocation asks for the satellite
// claim adapter.
func hasSDKStdioFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--sdk-stdio" {
			return true
		}
	}
	return false
}

// runSDKStdioCommand serves one host claim on stdio. Only the bare
// --sdk-stdio flag is admitted: any other argument is a miswired adapter
// invocation and fails closed instead of serving under surprising flags.
func runSDKStdioCommand(ctx context.Context, args []string) error {
	for _, arg := range args {
		if arg != "--sdk-stdio" {
			return fmt.Errorf("collector --sdk-stdio takes no other arguments, got %q", arg)
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	return serveSDKStdio(ctx, os.Stdin, os.Stdout, sdkStdioDeps{getenv: os.Getenv, logger: logger})
}

// encodeSDKStdioResult writes one indented SDK result: the ack the host
// decodes and commits.
func encodeSDKStdioResult(stdout io.Writer, result sdkcollector.Result) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("sdk-stdio encodes the result: %w", err)
	}
	return nil
}
