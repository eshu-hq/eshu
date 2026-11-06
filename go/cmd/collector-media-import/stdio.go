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

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
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
// drained envelopes commit into the SDK result for the host's
// grant-checked commit.
func runSDKStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return serveStdioClaim(ctx, stdin, stdout, os.Getenv, openProductionSource)
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
// result to stdout. The claim's generation drains through the existing
// directory source and drain loop unchanged; the drained envelopes are
// committed into the SDK result, which travels the grant-checked SDK path:
// the result is self-checked against the host contract the claim carries,
// and the host applies producer-grant coverage plus its durable commit
// before completing the claim. The satellite performs no grant evaluation
// itself (it holds no registry) and no direct store write (it runs without
// network); any failure exits nonzero with no ack on stdout.
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
	src, err := openSource(cfg, logger)
	if err != nil {
		return err
	}
	capture := &captureCommitter{}
	generations, _, err := drainOnce(ctx, src, capture, logger)
	if err != nil {
		return err
	}
	result, err := ackResult(request, capture, generations)
	if err != nil {
		return err
	}
	if _, err := sdkcollector.NewValidator(request.Contract).ValidateResult(result); err != nil {
		return fmt.Errorf("validate claim result against host contract: %w", err)
	}
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
			// item's scope and generation, and the SDK contract
			// requires the refs to match. The drain's
			// content-derived identity survives in the stable
			// key and payload, so re-runs stay idempotent.
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
