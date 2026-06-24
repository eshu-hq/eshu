// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// cortexScorecard is the typed projection of one Cortex scorecard descriptor
// (scorecard-as-code). A scorecard is its own YAML file declaring a tag, name,
// a ladder of levels, and a list of rules; an exported catalog may also carry
// per-entity results. Only the fields the producer maps into facts are modeled.
type cortexScorecard struct {
	Tag         string                  `yaml:"tag"`
	Name        string                  `yaml:"name"`
	Description string                  `yaml:"description"`
	Draft       bool                    `yaml:"draft"`
	Rules       []cortexScorecardRule   `yaml:"rules"`
	Results     []cortexScorecardResult `yaml:"results"`
}

// cortexScorecardRule models one rules entry: a titled check with an expression,
// a stable identifier, and the ladder level it gates.
type cortexScorecardRule struct {
	Title          string `yaml:"title"`
	Identifier     string `yaml:"identifier"`
	Expression     string `yaml:"expression"`
	Level          string `yaml:"level"`
	FailureMessage string `yaml:"failureMessage"`
}

// cortexScorecardResult models one per-entity scorecard score from an exported
// catalog: the evaluated entity tag and type, the achieved ladder level, and a
// numeric score.
type cortexScorecardResult struct {
	Tag   string `yaml:"tag"`
	Type  string `yaml:"type"`
	Level string `yaml:"level"`
	Score any    `yaml:"score"`
}

// CortexScorecardEnvelopes normalizes one offline Cortex scorecard descriptor
// into service_catalog.scorecard_definition facts (one per rule) and
// service_catalog.scorecard_result facts (one per declared per-entity result).
//
// These fact kinds are carried for read-surface completeness and forward
// compatibility: the shipped reducer index does not consume scorecards yet, so
// they MUST NOT change any entity's correlation outcome. The producer never
// fabricates service or workload identity from a scorecard. A scorecard with no
// tag yields a warning rather than a silent drop.
func CortexScorecardEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	envelopes := make([]facts.Envelope, 0)
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	docIndex := 0
	for {
		var scorecard cortexScorecard
		err := decoder.Decode(&scorecard)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			envelopes = append(envelopes, warningEnvelope(ctx, ProviderCortex, "",
				"invalid_document", fmt.Sprintf("cortex scorecard document %d failed to parse: %v", docIndex, err)))
			docIndex++
			continue
		}
		docIndex++
		envelopes = append(envelopes, cortexScorecardDocumentEnvelopes(ctx, scorecard)...)
	}
	return deduplicateEnvelopes(envelopes), nil
}

// cortexScorecardDocumentEnvelopes turns one parsed scorecard into facts.
func cortexScorecardDocumentEnvelopes(ctx FixtureContext, scorecard cortexScorecard) []facts.Envelope {
	scorecardTag := strings.TrimSpace(scorecard.Tag)
	if scorecardTag == "" {
		return []facts.Envelope{warningEnvelope(ctx, ProviderCortex, "",
			"invalid_ref", "cortex scorecard omitted tag; cannot anchor scorecard facts")}
	}

	var out []facts.Envelope
	seenRules := make(map[string]bool)
	for _, rule := range scorecard.Rules {
		identifier := scorecardRuleIdentifier(rule)
		if identifier == "" || seenRules[identifier] {
			continue
		}
		seenRules[identifier] = true
		out = append(out, cortexScorecardDefinitionEnvelope(ctx, scorecard, scorecardTag, rule, identifier))
	}

	seenResults := make(map[string]bool)
	for _, result := range scorecard.Results {
		ref := cortexResultEntityRef(result)
		if ref == "" || seenResults[ref] {
			continue
		}
		seenResults[ref] = true
		out = append(out, cortexScorecardResultEnvelope(ctx, scorecardTag, ref, result))
	}
	return out
}

// scorecardRuleIdentifier returns a stable rule identifier, falling back to the
// rule title when no explicit identifier is declared.
func scorecardRuleIdentifier(rule cortexScorecardRule) string {
	if id := strings.TrimSpace(rule.Identifier); id != "" {
		return id
	}
	return strings.TrimSpace(rule.Title)
}

// cortexResultEntityRef anchors a scorecard result on the evaluated entity using
// the same `type:cortex/tag` ref the entity producer mints, so a future reducer
// extension can join results to entities by provider plus entity_ref.
func cortexResultEntityRef(result cortexScorecardResult) string {
	tag := strings.ToLower(strings.TrimSpace(result.Tag))
	if tag == "" {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(result.Type))
	if kind == "" {
		kind = cortexDefaultType
	}
	return kind + ":" + ProviderCortexNamespace + "/" + tag
}

// cortexScorecardDefinitionEnvelope emits one service_catalog.scorecard_definition
// fact for a single rule. The reducer index does not consume it yet; it anchors
// on provider plus the scorecard and rule identity for forward compatibility.
func cortexScorecardDefinitionEnvelope(ctx FixtureContext, scorecard cortexScorecard, scorecardTag string, rule cortexScorecardRule, identifier string) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(ProviderCortex),
		"scorecard_tag":         scorecardTag,
		"scorecard_name":        strings.TrimSpace(scorecard.Name),
		"rule_identifier":       identifier,
		"rule_title":            strings.TrimSpace(rule.Title),
		"expression":            strings.TrimSpace(rule.Expression),
		"level":                 strings.TrimSpace(rule.Level),
	}
	stableKey := facts.StableID(facts.ServiceCatalogScorecardDefinitionFactKind, map[string]any{
		"provider":        string(ProviderCortex),
		"rule_identifier": identifier,
		"scorecard_tag":   scorecardTag,
	})
	return newEnvelope(ctx, facts.ServiceCatalogScorecardDefinitionFactKind, stableKey, scorecardTag+":"+identifier, payload)
}

// cortexScorecardResultEnvelope emits one service_catalog.scorecard_result fact
// for one entity's score. It carries the entity_ref anchor and never mints
// canonical service or workload identity from the score.
func cortexScorecardResultEnvelope(ctx FixtureContext, scorecardTag, entityRef string, result cortexScorecardResult) facts.Envelope {
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(ProviderCortex),
		"entity_ref":            entityRef,
		"scorecard_tag":         scorecardTag,
		"level":                 strings.TrimSpace(result.Level),
	}
	if score := normalizeScorecardScore(result.Score); score != "" {
		payload["score"] = score
	}
	stableKey := facts.StableID(facts.ServiceCatalogScorecardResultFactKind, map[string]any{
		"entity_ref":    entityRef,
		"provider":      string(ProviderCortex),
		"scorecard_tag": scorecardTag,
	})
	return newEnvelope(ctx, facts.ServiceCatalogScorecardResultFactKind, stableKey, scorecardTag+":"+entityRef, payload)
}

// normalizeScorecardScore renders a YAML-decoded score (int, float, or string)
// into a stable string. Integral floats are rendered without a decimal point so
// re-emission stays idempotent regardless of how YAML typed the scalar.
//
// FormatFloat with the 'f' verb and precision -1 already emits the shortest
// decimal that round-trips, so 100.0 renders as "100" and 100.5 as "100.5". It
// is used directly instead of routing integral floats through an int64
// conversion: converting an out-of-range float64 to int64 is
// implementation-defined in Go and could yield an unstable value, while
// FormatFloat renders even an out-of-range magnitude as a stable full-precision
// decimal string.
func normalizeScorecardScore(score any) string {
	switch value := score.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}
