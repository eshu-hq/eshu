// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
)

const (
	// windowFreshnessHint marks generations the emitter never observed.
	// The hint is free text on the wire; the host keys freshness on the
	// generation ID, so the value only tells operators no source was read.
	windowFreshnessHint = "synth-zero-fact"
	// maxRequestBytes caps one stdin request. The host's request is a
	// bounded claim envelope; anything past the cap fails closed instead
	// of allocating unbounded input.
	maxRequestBytes = 4 << 20
)

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "collector-synth-zero: %v\n", err)
		os.Exit(1)
	}
}

// serve reads one host request and writes one unchanged result. Stderr stays
// silent on success: operator visibility for the window comes from the
// host's status telemetry, which records the emitted status row per claim.
func serve(stdin io.Reader, stdout io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if len(raw) > maxRequestBytes {
		return fmt.Errorf("request exceeds %d bytes", maxRequestBytes)
	}
	var request extensionhost.Request
	if err := json.Unmarshal(raw, &request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	result := unchangedResult(request)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

// unchangedResult builds the window's only emission: the host claim echoed
// verbatim with an unchanged state, one complete status row with zero facts,
// and no page envelope. Echoing the request protocol version keeps the
// result on the contract the host asked with.
func unchangedResult(request extensionhost.Request) sdkcollector.Result {
	protocol := request.ProtocolVersion
	if protocol == "" {
		protocol = sdkcollector.ProtocolVersionV1Alpha1
	}
	return sdkcollector.Result{
		ProtocolVersion: protocol,
		State:           sdkcollector.ResultUnchanged,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:            request.Claim.GenerationID,
			ObservedAt:    time.Now().UTC(),
			FreshnessHint: windowFreshnessHint,
		},
		Statuses: []sdkcollector.Status{{Class: sdkcollector.StatusComplete, FactCount: 0}},
	}
}
