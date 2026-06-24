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

	scorecard "github.com/eshu-hq/eshu/examples/collector-extensions/scorecard"
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
		fail(err)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("scorecard-collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "testdata/complete.json", "Scorecard JSON input file")
	sourceURI := flags.String("source-uri", "https://api.securityscorecards.dev/projects/github.com/example/widgets", "safe source URI for emitted facts")
	previousDigest := flags.String("previous-digest", "", "previous report digest for unchanged detection")
	sdkStdio := flags.Bool("sdk-stdio", false, "Read one collector SDK host request from stdin")
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

func collectLocalFlags(inputPath string, sourceURI string, previousDigest string) (sdk.Result, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	report, err := scorecard.LoadReport(file)
	if err != nil {
		return sdk.Result{}, err
	}
	result, err := scorecard.Collect(demoClaim(), report, scorecard.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      sourceURI,
		PreviousDigest: previousDigest,
	})
	if err != nil {
		return sdk.Result{}, err
	}
	return result, nil
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
	sourceURI, err := nestedString(request.Config, "source", "sourceURI")
	if err != nil && !isMissingConfig(err) {
		return sdk.Result{}, err
	}
	previousDigest, err := nestedString(request.Config, "freshness", "previousDigest")
	if err != nil && !isMissingConfig(err) {
		return sdk.Result{}, err
	}

	file, err := os.Open(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	defer func() {
		_ = file.Close()
	}()
	report, err := scorecard.LoadReport(file)
	if err != nil {
		return sdk.Result{}, err
	}
	return scorecard.Collect(request.Claim, report, scorecard.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      sourceURI,
		PreviousDigest: previousDigest,
	})
}

type missingConfigError string

func (e missingConfigError) Error() string {
	return string(e)
}

func isMissingConfig(err error) bool {
	_, ok := err.(missingConfigError)
	return ok
}

func nestedString(config map[string]any, section string, key string) (string, error) {
	rawSection, ok := config[section]
	if !ok {
		return "", missingConfigError(fmt.Sprintf("config.%s is required", section))
	}
	sectionMap, ok := rawSection.(map[string]any)
	if !ok {
		return "", fmt.Errorf("config.%s must be an object", section)
	}
	rawValue, ok := sectionMap[key]
	if !ok {
		return "", missingConfigError(fmt.Sprintf("config.%s.%s is required", section, key))
	}
	value, ok := rawValue.(string)
	if !ok {
		return "", fmt.Errorf("config.%s.%s must be a string", section, key)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("config.%s.%s must not be empty", section, key)
	}
	return strings.TrimSpace(value), nil
}

func demoClaim() sdk.Claim {
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID:   scorecard.ComponentID,
		InstanceID:    "scorecard-local",
		CollectorKind: scorecard.CollectorKind,
		SourceSystem:  scorecard.SourceSystem,
		Scope: sdk.Scope{
			ID:   "github.com/example/widgets",
			Kind: "repository",
		},
		SourceRunID:  "run-local",
		GenerationID: "generation-local",
		WorkItemID:   "work-local",
		FencingToken: "fence-local",
		Attempt:      1,
		Deadline:     now.Add(5 * time.Minute),
		ConfigHandle: "config://examples/scorecard/local",
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "scorecard-collector: %v\n", err)
	os.Exit(1)
}
