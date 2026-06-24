// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	warningKindUnsupportedCompositeAttribute = "unsupported_composite_attribute"
	warningKindCompositeAttributeSkipped     = "composite_attribute_skipped"
)

type warningPayload struct {
	WarningKind string
	Reason      string
	Source      string
	Details     map[string]any
}

type compositeAttributeWarningKey struct {
	ResourceType string
	AttributeKey string
	Reason       string
}

type compositeAttributeWarningSummary struct {
	Count int64
}

func (p *stateParser) addSourceWarnings(warnings []SourceWarning) error {
	for _, warning := range warnings {
		payload := warningPayload{
			WarningKind: strings.TrimSpace(warning.WarningKind),
			Reason:      strings.TrimSpace(warning.Reason),
			Source:      strings.TrimSpace(warning.Source),
			Details:     warning.Details,
		}
		if payload.WarningKind == "" || payload.Reason == "" || payload.Source == "" {
			continue
		}
		if err := p.emitWarning(payload); err != nil {
			return err
		}
	}
	return nil
}

func (p *stateParser) recordRedaction(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	if p.redactions == nil {
		p.redactions = map[string]int64{}
	}
	p.redactions[reason]++
}

func (p *stateParser) emitWarning(warning warningPayload) error {
	if warning.WarningKind == "" || warning.Reason == "" || warning.Source == "" {
		return nil
	}
	payload := map[string]any{
		"warning_kind": warning.WarningKind,
		"reason":       warning.Reason,
		"source":       warning.Source,
	}
	if classification, ok := ClassifyWarning(warning.WarningKind, warning.Reason); ok {
		payload["severity"] = classification.Severity
		payload["actionability"] = classification.Actionability
	}
	for key, value := range warning.Details {
		switch key {
		case "warning_kind", "reason", "source", "severity", "actionability":
			continue
		default:
			payload[key] = value
		}
	}
	key := "warning:" + warning.WarningKind + ":" + warning.Source + ":" + warning.Reason
	if err := p.emitBodyFact(p.envelope(facts.TerraformStateWarningFactKind, key, payload, warning.Source)); err != nil {
		return err
	}
	if p.warningsByKind == nil {
		p.warningsByKind = map[string]int64{}
	}
	p.warningsByKind[warning.WarningKind]++
	return nil
}

func (p *stateParser) recordCompositeAttributeWarning(resourceType string, attributeKey string, reason string) {
	resourceType = strings.TrimSpace(resourceType)
	attributeKey = strings.TrimSpace(attributeKey)
	reason = strings.TrimSpace(reason)
	if resourceType == "" || attributeKey == "" || reason == "" {
		return
	}
	key := compositeAttributeWarningKey{
		ResourceType: resourceType,
		AttributeKey: attributeKey,
		Reason:       reason,
	}
	if p.compositeWarnings == nil {
		p.compositeWarnings = map[compositeAttributeWarningKey]*compositeAttributeWarningSummary{}
	}
	summary, ok := p.compositeWarnings[key]
	if !ok {
		summary = &compositeAttributeWarningSummary{}
		p.compositeWarnings[key] = summary
	}
	summary.Count++
}

func (p *stateParser) flushCompositeAttributeWarnings() error {
	if len(p.compositeWarnings) == 0 {
		return nil
	}
	keys := make([]compositeAttributeWarningKey, 0, len(p.compositeWarnings))
	for key := range p.compositeWarnings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ResourceType != keys[j].ResourceType {
			return keys[i].ResourceType < keys[j].ResourceType
		}
		if keys[i].AttributeKey != keys[j].AttributeKey {
			return keys[i].AttributeKey < keys[j].AttributeKey
		}
		return keys[i].Reason < keys[j].Reason
	})
	for _, key := range keys {
		summary := p.compositeWarnings[key]
		if summary == nil || summary.Count <= 0 {
			continue
		}
		if err := p.emitWarning(warningPayload{
			WarningKind: compositeAttributeWarningKind(key.Reason),
			Reason:      key.Reason,
			Source:      compositeAttributeWarningSource(key.ResourceType, key.AttributeKey),
			Details: map[string]any{
				"resource_type":    key.ResourceType,
				"attribute_key":    key.AttributeKey,
				"occurrence_count": summary.Count,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func compositeAttributeWarningKind(reason string) string {
	if reason == CompositeCaptureSkipReasonSchemaUnknown {
		return warningKindUnsupportedCompositeAttribute
	}
	return warningKindCompositeAttributeSkipped
}

func compositeAttributeWarningSource(resourceType string, attributeKey string) string {
	return "resources." + strings.TrimSpace(resourceType) + ".attributes." + strings.TrimSpace(attributeKey)
}
