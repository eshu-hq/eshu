// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	collector "github.com/eshu-hq/eshu-collector-template"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

type sdkRequest struct {
	ProtocolVersion string         `json:"protocol_version"`
	Claim           sdk.Claim      `json:"claim"`
	Contract        sdk.Contract   `json:"contract"`
	Config          map[string]any `json:"config,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "testdata/complete.json", "source JSON input file")
	sourceURI := flags.String("source-uri", "https://example.invalid/source/template", "credential-free source URI")
	previousDigest := flags.String("previous-digest", "", "previous report digest for unchanged detection")
	maxRecords := flags.Int("max-records", 0, "max records per claim (0 selects the default bound)")
	maxPayloadBytes := flags.Int("max-payload-bytes", 0, "max payload bytes per fact (0 selects the default bound)")
	claimTimeoutSeconds := flags.Int("claim-timeout-seconds", 0, "claim timeout in seconds (0 selects the default bound)")
	sdkStdio := flags.Bool("sdk-stdio", false, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	limits := collector.DefaultResourceUse()
	if *maxRecords > 0 {
		limits.MaxRecordsPerClaim = *maxRecords
	}
	if *maxPayloadBytes > 0 {
		limits.MaxPayloadBytes = *maxPayloadBytes
	}
	if *claimTimeoutSeconds > 0 {
		limits.ClaimTimeoutSeconds = *claimTimeoutSeconds
	}
	var result sdk.Result
	var err error
	if *sdkStdio {
		result, err = collectSDKStdio(stdin)
	} else {
		result, err = collectLocalFlags(*inputPath, *sourceURI, *previousDigest, limits)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func collectLocalFlags(inputPath, sourceURI, previousDigest string, limits collector.ResourceUse) (sdk.Result, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	defer func() { _ = file.Close() }()
	report, err := collector.LoadReport(file)
	if err != nil {
		return sdk.Result{}, err
	}
	return collector.Collect(demoClaim(), report, collector.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      sourceURI,
		PreviousDigest: previousDigest,
		Limits:         limits,
	})
}

func collectSDKStdio(stdin io.Reader) (sdk.Result, error) {
	var request sdkRequest
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return sdk.Result{}, fmt.Errorf("decode SDK request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return sdk.Result{}, fmt.Errorf("decode SDK request: trailing JSON value")
		}
		return sdk.Result{}, fmt.Errorf("decode SDK request trailer: %w", err)
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return sdk.Result{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	inputPath, err := nestedString(request.Config, "source", "input")
	if err != nil {
		return sdk.Result{}, err
	}
	// A missing key falls back to the placeholder; a present-but-malformed
	// value fails closed instead of silently emitting against the placeholder.
	sourceURI, present, err := nestedOptionalString(request.Config, "source", "sourceURI")
	if err != nil {
		return sdk.Result{}, err
	}
	if !present || strings.TrimSpace(sourceURI) == "" {
		sourceURI = "https://example.invalid/source/template"
	}
	previousDigest, _, err := nestedOptionalString(request.Config, "freshness", "previousDigest")
	if err != nil {
		return sdk.Result{}, err
	}
	limits, err := nestedLimits(request.Config)
	if err != nil {
		return sdk.Result{}, err
	}
	file, err := os.Open(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	defer func() { _ = file.Close() }()
	report, err := collector.LoadReport(file)
	if err != nil {
		return sdk.Result{}, err
	}
	claim := request.Claim
	if strings.TrimSpace(claim.GenerationID) == "" {
		claim.GenerationID = "generation-1"
	}
	return collector.Collect(claim, report, collector.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      sourceURI,
		PreviousDigest: previousDigest,
		Limits:         limits,
	})
}

func demoClaim() sdk.Claim {
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID:   collector.ComponentID,
		InstanceID:    "template-primary",
		CollectorKind: collector.CollectorKind,
		SourceSystem:  collector.SourceSystem,
		Scope:         sdk.Scope{ID: "component:template-primary", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "fence-1",
		Attempt:       1,
		Deadline:      now.Add(time.Hour),
		ConfigHandle:  "component-config:template",
	}
}

func nestedString(config map[string]any, keys ...string) (string, error) {
	value, present, err := nestedOptionalString(config, keys...)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("config %q missing", strings.Join(keys, "."))
	}
	return value, nil
}

// nestedOptionalString separates "missing" (present=false, err=nil, caller
// may default) from "malformed" (err non-nil, caller must fail closed).
func nestedOptionalString(config map[string]any, keys ...string) (string, bool, error) {
	var current any = config
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false, nil
		}
		current, ok = m[key]
		if !ok {
			return "", false, nil
		}
	}
	s, ok := current.(string)
	if !ok {
		return "", true, fmt.Errorf("config %q must be a string", strings.Join(keys, "."))
	}
	return s, true, nil
}

// nestedLimits reads the optional limits block from config.example.yaml's
// shape (limits.maxRecordsPerClaim, limits.maxPayloadBytes,
// limits.claimTimeoutSeconds). Absent or partial blocks select
// DefaultResourceUse for the missing fields; malformed values fail closed.
func nestedLimits(config map[string]any) (collector.ResourceUse, error) {
	limits := collector.DefaultResourceUse()
	for _, field := range []struct {
		key    string
		target *int
	}{
		{"maxRecordsPerClaim", &limits.MaxRecordsPerClaim},
		{"maxPayloadBytes", &limits.MaxPayloadBytes},
		{"claimTimeoutSeconds", &limits.ClaimTimeoutSeconds},
	} {
		raw, present, err := nestedOptionalNumber(config, "limits", field.key)
		if err != nil {
			return collector.ResourceUse{}, err
		}
		if present {
			*field.target = raw
		}
	}
	return limits, nil
}

func nestedOptionalNumber(config map[string]any, keys ...string) (int, bool, error) {
	var current any = config
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return 0, false, nil
		}
		current, ok = m[key]
		if !ok {
			return 0, false, nil
		}
	}
	switch number := current.(type) {
	case float64:
		if number != float64(int(number)) || number <= 0 {
			return 0, true, fmt.Errorf("config %q must be a positive integer", strings.Join(keys, "."))
		}
		return int(number), true, nil
	default:
		return 0, true, fmt.Errorf("config %q must be a number", strings.Join(keys, "."))
	}
}
