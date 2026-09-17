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
	sdkStdio := flags.Bool("sdk-stdio", false, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var result sdk.Result
	var err error
	if *sdkStdio {
		result, err = collectSDKStdio(stdin)
	} else {
		result, err = collectLocalFlags(*inputPath, *sourceURI, *previousDigest)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func collectLocalFlags(inputPath, sourceURI, previousDigest string) (sdk.Result, error) {
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
	sourceURI, _ := nestedString(request.Config, "source", "sourceURI")
	if strings.TrimSpace(sourceURI) == "" {
		sourceURI = "https://example.invalid/source/template"
	}
	previousDigest, _ := nestedString(request.Config, "freshness", "previousDigest")
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
	var current any = config
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("config %q missing", strings.Join(keys, "."))
		}
		current, ok = m[key]
		if !ok {
			return "", fmt.Errorf("config %q missing", strings.Join(keys, "."))
		}
	}
	s, ok := current.(string)
	if !ok {
		return "", fmt.Errorf("config %q must be a string", strings.Join(keys, "."))
	}
	return s, nil
}
