// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/sbom/document"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// pinnedSBOM is the single CycloneDX document this v1 leaf extracts on every
// call: a byte-identical copy of the lodash fixture behind the document
// package's checked-in assertions. TestPinnedFixtureMatchesSource fails on
// any drift, so a re-pin is always deliberate. The live-read half is a later
// leaf; v1 never touches the network for document bytes.
//
//go:embed testdata/cyclonedx_image_subject.json
var pinnedSBOM []byte

const (
	// maxRequestBytes caps one stdin request. The host's request is a
	// bounded claim envelope; anything past the cap fails closed instead
	// of allocating unbounded input.
	maxRequestBytes = 4 << 20
	// pinnedSourceURI is the provenance URI recorded on every emitted fact.
	// The .invalid TLD (RFC 2606) marks it as a non-resolvable pin: v1
	// reads baked-in bytes, never this address. It must stay a parseable
	// non-file URI because the host validator rejects blank and
	// file-scheme source_ref.uri values.
	pinnedSourceURI = "https://eshu.invalid/pinned/sbom/cyclonedx-image-subject.json"
	// shadowFreshnessHint marks generations the emitter never observed a
	// live source for. The hint is free text on the wire; the host keys
	// freshness on the generation ID, so the value only tells operators
	// the facts came from the pinned document.
	shadowFreshnessHint = "sbom-shadow-pinned-document"
)

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "collector-sbom-shadow: %v\n", err)
		os.Exit(1)
	}
}

// serve reads one host request and writes one complete result carrying the
// pinned document's parsed envelopes as SDK facts. Stderr stays silent on
// success: operator visibility for the window comes from the host's status
// telemetry. Every failure names counts and knob names only, never request
// or document values, because the host relays stderr next to claim records.
func serve(stdin io.Reader, stdout io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if len(raw) > maxRequestBytes {
		return fmt.Errorf("request exceeds %d bytes (maxRequestBytes)", maxRequestBytes)
	}
	var request extensionhost.Request
	if err := json.Unmarshal(raw, &request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	fencingToken, err := parseFencingToken(request.Claim.FencingToken)
	if err != nil {
		return err
	}
	observedAt := time.Now().UTC()
	envelopes, err := document.CycloneDXFixtureEnvelopes(pinnedSBOM, document.FixtureContext{
		ScopeID:             request.Claim.Scope.ID,
		GenerationID:        request.Claim.GenerationID,
		CollectorInstanceID: request.Claim.InstanceID,
		FencingToken:        fencingToken,
		ObservedAt:          observedAt,
		SourceURI:           pinnedSourceURI,
	})
	if err != nil {
		return fmt.Errorf("parse pinned sbom: %w", err)
	}
	result := completeResult(request, envelopes, observedAt)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

// parseFencingToken converts the claim's decimal fencing token for the
// parser boundary. It names the knob without echoing the value so a corrupt
// token cannot reach stderr next to claim records.
func parseFencingToken(raw string) (int64, error) {
	fencingToken, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("claim.fencing_token must be a base-10 integer")
	}
	return fencingToken, nil
}

// completeResult builds the window's only emission: the host claim echoed
// verbatim with state complete, no status rows, and the generation ID taken
// from the incoming claim as host validation requires. The single-call
// commit lane records no statuses, so even a complete summary row would turn
// every commit away at the lane door; the count stays derivable from the
// facts. Echoing the request protocol version keeps the result on the
// contract the host asked with.
func completeResult(request extensionhost.Request, envelopes []facts.Envelope, observedAt time.Time) sdkcollector.Result {
	protocol := request.ProtocolVersion
	if protocol == "" {
		protocol = sdkcollector.ProtocolVersionV1Alpha1
	}
	factsOut := make([]sdkcollector.Fact, 0, len(envelopes))
	for _, envelope := range envelopes {
		factsOut = append(factsOut, sdkFact(envelope))
	}
	return sdkcollector.Result{
		ProtocolVersion: protocol,
		State:           sdkcollector.ResultComplete,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:            request.Claim.GenerationID,
			ObservedAt:    observedAt,
			FreshnessHint: shadowFreshnessHint,
		},
		Facts: factsOut,
	}
}

// sdkFact maps one parser envelope onto the collector SDK fact shape. The
// scope, generation, and source-system boundaries come from the parser, which
// derived them from the claim, so host validation sees claim-matching
// references without the leaf rewriting provenance.
func sdkFact(envelope facts.Envelope) sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             envelope.FactKind,
		SchemaVersion:    envelope.SchemaVersion,
		StableKey:        envelope.StableFactKey,
		SourceConfidence: sdkcollector.SourceConfidence(envelope.SourceConfidence),
		ObservedAt:       envelope.ObservedAt,
		Tombstone:        envelope.IsTombstone,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: envelope.SourceRef.SourceSystem,
			ScopeID:      envelope.SourceRef.ScopeID,
			GenerationID: envelope.SourceRef.GenerationID,
			FactKey:      envelope.SourceRef.FactKey,
			URI:          envelope.SourceRef.SourceURI,
			RecordID:     envelope.SourceRef.SourceRecordID,
		},
		Payload: envelope.Payload,
	}
}
