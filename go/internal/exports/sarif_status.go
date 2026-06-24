// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import "strings"

const (
	sarifRuleEvidenceIncomplete = "ESHU-SCAN-EVIDENCE-INCOMPLETE"
	sarifRuleUnsupported        = "ESHU-SCAN-UNSUPPORTED"
)

func applySARIFStatusProperties(props *sarifProperties, status SnapshotStatus) {
	if props == nil {
		return
	}
	props.ReportSchemaVersion = strings.TrimSpace(status.ReportSchemaVersion)
	props.ReadinessState = strings.TrimSpace(status.ReadinessState)
	props.ReadinessFreshness = strings.TrimSpace(status.ReadinessFreshness)
	props.ExitCode = status.ExitCode
	props.ExitReason = strings.TrimSpace(status.ExitReason)
	props.ScopeMode = strings.TrimSpace(status.ScopeMode)
	props.MissingEvidence = cloneStrings(status.MissingEvidence)
	props.IncompleteReasons = cloneStrings(status.IncompleteReasons)
	props.UnsupportedTargets = sarifUnsupportedTargets(status.UnsupportedTargets)
}

func appendStatusResults(
	rules []sarifRule,
	ruleIndex map[string]int,
	results []sarifResult,
	status SnapshotStatus,
) ([]sarifRule, []sarifResult) {
	statusRuleID, level, message := sarifStatusRule(status)
	if statusRuleID == "" {
		return rules, results
	}
	if _, ok := ruleIndex[statusRuleID]; !ok {
		ruleIndex[statusRuleID] = len(rules)
		rules = append(rules, sarifRule{
			ID:   statusRuleID,
			Name: message,
			ShortDescription: &sarifMessage{
				Text: message,
			},
			DefaultConfiguration: &sarifConfiguration{Level: level},
			Properties: &sarifRuleProps{
				Severity: "none",
				Tags:     []string{"security", "vulnerability"},
			},
		})
	}
	results = append(results, sarifResult{
		RuleID:    statusRuleID,
		RuleIndex: ruleIndex[statusRuleID],
		Level:     level,
		Message: sarifMessage{
			Text: sarifStatusMessage(status),
		},
		PartialFingerprints: map[string]string{
			"eshu/readinessState/v1": strings.TrimSpace(status.ReadinessState),
		},
		Properties: &sarifResultProps{
			ScannerStatus: &sarifScannerStatusProp{
				ReadinessState:    strings.TrimSpace(status.ReadinessState),
				MissingEvidence:   cloneStrings(status.MissingEvidence),
				IncompleteReasons: cloneStrings(status.IncompleteReasons),
			},
		},
	})
	return rules, results
}

func sarifStatusRule(status SnapshotStatus) (ruleID string, level string, message string) {
	switch strings.TrimSpace(status.ReadinessState) {
	case "unsupported":
		return sarifRuleUnsupported, "error", "Eshu vulnerability scan unsupported target evidence"
	case "not_configured", "target_incomplete", "evidence_incomplete", "readiness_unavailable":
		return sarifRuleEvidenceIncomplete, "warning", "Eshu vulnerability scan evidence incomplete"
	default:
		return "", "", ""
	}
}

func sarifStatusMessage(status SnapshotStatus) string {
	state := strings.TrimSpace(status.ReadinessState)
	if state == "" {
		state = "readiness_unavailable"
	}
	var builder strings.Builder
	builder.WriteString("Eshu vulnerability scan did not reach a clean ready result: ")
	builder.WriteString(state)
	if len(status.MissingEvidence) > 0 {
		builder.WriteString("; missing evidence: ")
		builder.WriteString(strings.Join(status.MissingEvidence, ", "))
	}
	if len(status.IncompleteReasons) > 0 {
		builder.WriteString("; incomplete: ")
		builder.WriteString(strings.Join(status.IncompleteReasons, ", "))
	}
	return builder.String()
}

func sarifUnsupportedTargets(in []UnsupportedTarget) []sarifUnsupportedTargetJS {
	if len(in) == 0 {
		return nil
	}
	out := make([]sarifUnsupportedTargetJS, 0, len(in))
	for _, target := range in {
		out = append(out, sarifUnsupportedTargetJS(target))
	}
	return out
}
