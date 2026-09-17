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
	if *maxRecords < 0 || *maxPayloadBytes < 0 || *claimTimeoutSeconds < 0 {
		return fmt.Errorf("limits must be non-negative (0 selects defaults)")
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
	// The health monitor is the operator signal behind health.go: every
	// outcome below is observed, and the snapshot line on stderr carries
	// liveness, freshness, failure, and retry state. Adopters graduate this
	// to their logs/metrics/status endpoint; the shape stays the same.
	// The monitor reports the effective limits for the mode in use, and
	// collection runs under a wall-time bound in both modes: breaching it
	// fails the run instead of emitting late evidence.
	monitor := collector.NewMonitor()
	var result sdk.Result
	var err error
	if *sdkStdio {
		var plan stdioPlan
		plan, err = parseSDKStdio(stdin)
		if err == nil {
			limits = plan.limits
			result, err = runWithTimeout(func() (sdk.Result, error) {
				return collectStdioPlan(plan)
			}, limits)
		}
	} else {
		result, err = collectWithTimeout(*inputPath, *sourceURI, *previousDigest, limits)
	}
	monitor.SetResource(limits)
	if err != nil {
		monitor.ObserveFailure(firstLine(err.Error()))
		health, resource := monitor.Snapshot()
		logHealth(stderr, health, resource)
		return err
	}
	// Only Complete/Partial with a snapshot digest move LastSuccessAt.
	// Terminal outcomes observe as failures (propagating the failure
	// class), and Unchanged outcomes leave the last good digest alone.
	digest := ""
	for _, fact := range result.Facts {
		if fact.Kind == collector.FactKindSnapshot {
			digest = fact.StableKey
		}
	}
	if result.State == sdk.ResultTerminal {
		monitor.ObserveFailure(terminalCause(result))
	} else if digest != "" {
		monitor.ObserveSuccess(digest, time.Now().UTC())
	}
	health, resource := monitor.Snapshot()
	logHealth(stderr, health, resource)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// terminalCause propagates the terminal failure class for operator
// visibility instead of a static string.
func terminalCause(result sdk.Result) string {
	for _, status := range result.Statuses {
		if strings.TrimSpace(status.FailureClass) != "" {
			return firstLine(status.FailureClass)
		}
	}
	return "claim-terminal"
}

func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func logHealth(stderr io.Writer, health collector.Health, resource collector.ResourceUse) {
	// Error and digest strings here never carry credentials: collection
	// errors never echo URIs or payloads, and digests are opaque hashes.
	line, err := json.Marshal(map[string]any{"health": health, "resource": resource})
	if err != nil {
		return
	}
	fmt.Fprintf(stderr, "collector health: %s\n", line)
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

// collectWithTimeout bounds one local-flags claim's wall time: a
// collection that outruns ClaimTimeoutSeconds fails the run instead of
// emitting late evidence. Single-shot mode performs no retries; adopters
// adding a retry loop bound it with Monitor.ObserveRetryNevertheless.
func collectWithTimeout(inputPath, sourceURI, previousDigest string, limits collector.ResourceUse) (sdk.Result, error) {
	return runWithTimeout(func() (sdk.Result, error) {
		return collectLocalFlags(inputPath, sourceURI, previousDigest, limits)
	}, limits)
}

// runWithTimeout bounds one claim's wall time whatever the entry mode: a
// collection that outruns limits.ClaimTimeoutSeconds fails the run instead
// of emitting late evidence. Limits reaching here always carry a positive
// timeout (flag parsing and nestedLimits default per field), so the bound
// is never accidentally zero.
func runWithTimeout(work func() (sdk.Result, error), limits collector.ResourceUse) (sdk.Result, error) {
	type outcome struct {
		result sdk.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := work()
		done <- outcome{result: result, err: err}
	}()
	timeout := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	select {
	case finished := <-done:
		return finished.result, finished.err
	case <-time.After(timeout):
		return sdk.Result{}, fmt.Errorf("claim wall-time bound of %d seconds exceeded", limits.ClaimTimeoutSeconds)
	}
}

// stdioPlan is a parsed SDK host request: everything parseSDKStdio learns
// from stdin before the wall-time bound starts covering the collection.
type stdioPlan struct {
	inputPath      string
	sourceURI      string
	previousDigest string
	limits         collector.ResourceUse
	claim          sdk.Claim
}

// parseSDKStdio decodes and validates one collector SDK host request. It
// performs no collection itself so run() can report the effective limits
// and enforce their wall-time bound around the collect step.
func parseSDKStdio(stdin io.Reader) (stdioPlan, error) {
	plan := stdioPlan{limits: collector.DefaultResourceUse()}
	var request sdkRequest
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return stdioPlan{}, fmt.Errorf("decode SDK request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return stdioPlan{}, fmt.Errorf("decode SDK request: trailing JSON value")
		}
		return stdioPlan{}, fmt.Errorf("decode SDK request trailer: %w", err)
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return stdioPlan{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	inputPath, err := nestedString(request.Config, "source", "input")
	if err != nil {
		return stdioPlan{}, err
	}
	// A missing key falls back to the placeholder; a present-but-malformed
	// value fails closed instead of silently emitting against the placeholder.
	sourceURI, present, err := nestedOptionalString(request.Config, "source", "sourceURI")
	if err != nil {
		return stdioPlan{}, err
	}
	if !present || strings.TrimSpace(sourceURI) == "" {
		sourceURI = "https://example.invalid/source/template"
	}
	previousDigest, _, err := nestedOptionalString(request.Config, "freshness", "previousDigest")
	if err != nil {
		return stdioPlan{}, err
	}
	limits, err := nestedLimits(request.Config)
	if err != nil {
		return stdioPlan{}, err
	}
	claim := request.Claim
	if strings.TrimSpace(claim.GenerationID) == "" {
		claim.GenerationID = "generation-1"
	}
	plan.inputPath = inputPath
	plan.sourceURI = sourceURI
	plan.previousDigest = previousDigest
	plan.limits = limits
	plan.claim = claim
	return plan, nil
}

// collectStdioPlan runs the file read and Collect for a parsed request
// under the caller's wall-time bound.
func collectStdioPlan(plan stdioPlan) (sdk.Result, error) {
	file, err := os.Open(plan.inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	defer func() { _ = file.Close() }()
	report, err := collector.LoadReport(file)
	if err != nil {
		return sdk.Result{}, err
	}
	return collector.Collect(plan.claim, report, collector.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      plan.sourceURI,
		PreviousDigest: plan.previousDigest,
		Limits:         plan.limits,
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
			// A present-but-wrong-typed intermediate node fails closed;
			// only a genuinely absent (nil) node selects the default.
			if current != nil {
				return "", true, fmt.Errorf("config %q must be an object", strings.Join(keys, "."))
			}
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
			if current != nil {
				return 0, true, fmt.Errorf("config %q must be an object", strings.Join(keys, "."))
			}
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
