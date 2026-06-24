// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	pagerduty "github.com/eshu-hq/eshu/examples/collector-extensions/pagerduty"
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
	flags := flag.NewFlagSet("pagerduty-reference", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "testdata/complete.json", "Redacted PagerDuty observation fixture")
	sdkStdio := flags.Bool("sdk-stdio", false, "Read one collector SDK host request from stdin")
	proofDigest := flags.Bool("proof-digest", false, "Print a canonical proof digest instead of the full SDK result")
	proofDigestJSON := flags.Bool("proof-digest-json", false, "Read canonical proof facts JSON from stdin and print its digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *proofDigest && *proofDigestJSON {
		return fmt.Errorf("--proof-digest and --proof-digest-json are mutually exclusive")
	}
	if *proofDigestJSON {
		digest, err := digestProofFactsJSON(stdin)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, digest)
		return err
	}
	var result sdk.Result
	var err error
	if *sdkStdio {
		result, err = collectSDKStdio(stdin)
	} else {
		result, err = collectLocalFixture(*inputPath)
	}
	if err != nil {
		return err
	}
	if *proofDigest {
		digest, err := digestSDKFacts(result.Facts)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, digest)
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func collectLocalFixture(inputPath string) (sdk.Result, error) {
	observation, err := loadObservation(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	return pagerduty.Collect(demoClaim(observation.ObservedAt), observation)
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
	observation, err := loadObservation(inputPath)
	if err != nil {
		return sdk.Result{}, err
	}
	return pagerduty.Collect(request.Claim, observation)
}

func loadObservation(inputPath string) (pagerduty.Observation, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return pagerduty.Observation{}, err
	}
	defer func() {
		_ = file.Close()
	}()
	return pagerduty.LoadObservation(file)
}

func nestedString(config map[string]any, section string, key string) (string, error) {
	rawSection, ok := config[section]
	if !ok {
		return "", fmt.Errorf("config.%s is required", section)
	}
	sectionMap, ok := rawSection.(map[string]any)
	if !ok {
		return "", fmt.Errorf("config.%s must be an object", section)
	}
	rawValue, ok := sectionMap[key]
	if !ok {
		return "", fmt.Errorf("config.%s.%s is required", section, key)
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

type proofFact struct {
	Kind             string         `json:"kind"`
	SchemaVersion    string         `json:"schema_version"`
	StableKey        string         `json:"stable_key"`
	SourceConfidence string         `json:"source_confidence"`
	SourceRef        proofSourceRef `json:"source_ref"`
	Payload          any            `json:"payload"`
}

type proofSourceRef struct {
	SourceSystem string `json:"source_system"`
	ScopeID      string `json:"scope_id"`
	GenerationID string `json:"generation_id"`
	FactKey      string `json:"fact_key"`
	URI          string `json:"uri"`
	RecordID     string `json:"record_id"`
}

func digestSDKFacts(facts []sdk.Fact) (string, error) {
	proofFacts := make([]proofFact, 0, len(facts))
	for _, fact := range facts {
		proofFacts = append(proofFacts, proofFact{
			Kind:             fact.Kind,
			SchemaVersion:    fact.SchemaVersion,
			StableKey:        fact.StableKey,
			SourceConfidence: string(fact.SourceConfidence),
			SourceRef: proofSourceRef{
				SourceSystem: fact.SourceRef.SourceSystem,
				ScopeID:      fact.SourceRef.ScopeID,
				GenerationID: fact.SourceRef.GenerationID,
				FactKey:      fact.SourceRef.FactKey,
				URI:          fact.SourceRef.URI,
				RecordID:     fact.SourceRef.RecordID,
			},
			Payload: fact.Payload,
		})
	}
	return digestProofFacts(proofFacts)
}

func digestProofFactsJSON(stdin io.Reader) (string, error) {
	var facts []proofFact
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&facts); err != nil {
		return "", fmt.Errorf("decode proof facts JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("decode proof facts JSON: trailing JSON value")
		}
		return "", fmt.Errorf("decode proof facts JSON trailer: %w", err)
	}
	return digestProofFacts(facts)
}

func digestProofFacts(facts []proofFact) (string, error) {
	sort.Slice(facts, func(i int, j int) bool {
		if facts[i].Kind != facts[j].Kind {
			return facts[i].Kind < facts[j].Kind
		}
		return facts[i].StableKey < facts[j].StableKey
	})
	canonical, err := json.Marshal(facts)
	if err != nil {
		return "", fmt.Errorf("marshal proof facts: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func demoClaim(observedAt time.Time) sdk.Claim {
	return sdk.Claim{
		ComponentID:   pagerduty.ComponentID,
		InstanceID:    "pagerduty-reference-local",
		CollectorKind: pagerduty.CollectorKind,
		SourceSystem:  pagerduty.SourceSystem,
		Scope: sdk.Scope{
			ID:   "pagerduty:account:synthetic-reference",
			Kind: "pagerduty_account",
		},
		SourceRunID:  "pagerduty-reference-run-2026-06-14",
		GenerationID: "pagerduty-reference-generation-2026-06-14",
		WorkItemID:   "pagerduty-reference-work",
		FencingToken: "pagerduty-reference-fence",
		Attempt:      1,
		Deadline:     observedAt.Add(5 * time.Minute),
		ConfigHandle: "config://examples/pagerduty/reference",
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "pagerduty-reference: %v\n", err)
	os.Exit(1)
}
