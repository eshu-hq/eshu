// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

// WarningFactOptions describes one non-fatal Terraform state warning fact.
type WarningFactOptions struct {
	Scope        scope.IngestionScope
	Generation   scope.ScopeGeneration
	Source       StateKey
	ObservedAt   time.Time
	FencingToken int64
	Warning      SourceWarning
}

// NewWarningFact returns a Terraform-state warning envelope without reading
// state content. It is used for source-level warnings that happen before the
// streaming parser can construct normal snapshot facts.
func NewWarningFact(options WarningFactOptions) (facts.Envelope, error) {
	if err := options.Scope.Validate(); err != nil {
		return facts.Envelope{}, err
	}
	if err := options.Generation.ValidateForScope(options.Scope); err != nil {
		return facts.Envelope{}, err
	}
	if err := options.Source.Validate(); err != nil {
		return facts.Envelope{}, err
	}
	if options.ObservedAt.IsZero() {
		return facts.Envelope{}, fmt.Errorf("observed_at must not be zero")
	}
	if options.FencingToken <= 0 {
		return facts.Envelope{}, fmt.Errorf("fencing_token must be positive")
	}

	warningKind := strings.TrimSpace(options.Warning.WarningKind)
	reason := strings.TrimSpace(options.Warning.Reason)
	warningSource := strings.TrimSpace(options.Warning.Source)
	if warningKind == "" {
		return facts.Envelope{}, fmt.Errorf("warning_kind must not be blank")
	}
	if reason == "" {
		return facts.Envelope{}, fmt.Errorf("warning reason must not be blank")
	}
	if warningSource == "" {
		return facts.Envelope{}, fmt.Errorf("warning source must not be blank")
	}

	payload := map[string]any{
		"warning_kind": warningKind,
		"reason":       reason,
		"source":       warningSource,
	}
	if classification, ok := ClassifyWarning(warningKind, reason); ok {
		payload["severity"] = classification.Severity
		payload["actionability"] = classification.Actionability
	}
	for key, value := range options.Warning.Details {
		switch key {
		case "warning_kind", "reason", "source", "severity", "actionability":
			continue
		default:
			payload[key] = value
		}
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeTerraformStateWarning(tfstatev1.Warning{
			WarningKind:   warningKind,
			Reason:        reason,
			Source:        warningSource,
			Severity:      optionalStringPtr(payloadString(payload, "severity")),
			Actionability: optionalStringPtr(payloadString(payload, "actionability")),
		})
	}); err != nil {
		return facts.Envelope{}, err
	}
	key := "terraform_state_warning:warning:" + warningKind + ":" + warningSource + ":" + reason
	version, _ := facts.TerraformStateSchemaVersion(facts.TerraformStateWarningFactKind)
	return facts.Envelope{
		FactID: facts.StableID("TerraformStateFact", map[string]any{
			"fact_kind":     facts.TerraformStateWarningFactKind,
			"stable_key":    key,
			"scope_id":      options.Scope.ScopeID,
			"generation_id": options.Generation.GenerationID,
		}),
		ScopeID:          options.Scope.ScopeID,
		GenerationID:     options.Generation.GenerationID,
		FactKind:         facts.TerraformStateWarningFactKind,
		StableFactKey:    key,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorTerraformState),
		FencingToken:     options.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       options.ObservedAt.UTC(),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorTerraformState),
			ScopeID:        options.Scope.ScopeID,
			GenerationID:   options.Generation.GenerationID,
			FactKey:        key,
			SourceURI:      sourceURI(options.Source),
			SourceRecordID: warningSource,
		},
	}, nil
}
