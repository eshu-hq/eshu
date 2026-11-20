// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package census

import (
	"strings"
	"testing"
)

// driftStrikeExpr is the EshuAWSDriftConsecutiveCorrectingAudits expression
// as both deploy rule files must carry it verbatim (#11032): a correcting
// audit in the last 25h window AND in the preceding 25h window. The 25h
// windows are the 24h audit interval plus stagger slack; for is capped at
// 15m because the signal is daily and a longer for only delays the page.
// The single-window form without offset cannot tell a clean audit from no
// audit, so the planted-violation test below must catch its acceptance.
const driftStrikeExpr = `(sum(increase(eshu_dp_aws_drift_incremental_audits_total{outcome="correcting"}[25h])) > 0) and on() (sum(increase(eshu_dp_aws_drift_incremental_audits_total{outcome="correcting"}[25h] offset 25h)) > 0)`

// driftStrikeSingleWindowExpr is the degenerate form the checker must
// reject: one window without offset fires on any single correcting audit and
// proves nothing about consecutiveness.
const driftStrikeSingleWindowExpr = `sum(increase(eshu_dp_aws_drift_incremental_audits_total{outcome="correcting"}[25h])) > 0`

// driftAuditStrikeAlerts names the strike alert and the required expression
// fragments its rule must contain: the correcting-outcome selector (a plain
// corrections_total expression cannot tell a clean audit from no audit) and
// the second-window offset (a single window without offset fires on any one
// correcting audit and proves nothing about consecutiveness). This map drives
// the fixture tests below, which pin the exact contract the deploy PR had to
// satisfy, and the live tests at the bottom, which prove both deploy rule
// files at the pinned checkout still satisfy it.
var driftAuditStrikeAlerts = map[string][]string{
	"EshuAWSDriftConsecutiveCorrectingAudits": {
		`eshu_dp_aws_drift_incremental_audits_total{outcome="correcting"}`,
		`offset 25h`,
	},
}

const driftStrikeStandaloneFixture = `groups:
  - name: eshu.hosted_ops
    rules:
      - alert: EshuAWSDriftConsecutiveCorrectingAudits
        expr: ` + driftStrikeExpr + `
        for: 15m
        labels:
          severity: critical
          component: pipeline
          runbook_section: drift-audit
        annotations:
          summary: "Two consecutive drift audits corrected delta misses"
          description: "Two consecutive daily aws_cloud_runtime_drift audits each found keys the delta run would have missed: the skip path is systematically blind."
          runbook: "Inspect the delta filter/diff path the audit replays and the magnitude on eshu_dp_aws_drift_incremental_audit_corrections_total; never restart broadly to clear it."
`

const driftStrikeOperatorFixture = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: eshu-hosted-ops
spec:
  groups:
    - name: eshu.hosted_ops
      rules:
        - alert: EshuAWSDriftConsecutiveCorrectingAudits
          expr: ` + driftStrikeExpr + `
          for: 15m
          labels:
            severity: critical
            component: pipeline
            runbook_section: drift-audit
          annotations:
            summary: "Two consecutive drift audits corrected delta misses"
            description: "Two consecutive daily aws_cloud_runtime_drift audits each found keys the delta run would have missed: the skip path is systematically blind."
            runbook: "Inspect the delta filter/diff path the audit replays and the magnitude on eshu_dp_aws_drift_incremental_audit_corrections_total; never restart broadly to clear it."
`

// driftStrikeRegistered is the closed metric universe the strike expression
// may read: the denominator counter the reducer registers. The fixture tests
// use this hand-written set; the live tests below use registeredCensusMetrics
// so the check runs against the metric names telemetry actually exports.
var driftStrikeRegistered = map[string]bool{
	"eshu_dp_aws_drift_incremental_audits_total": true,
}

// checkDriftAuditStrikeAlerts returns every way the two rule files fail the
// #11032 strike contract, via the shared dual-file checker: the alert must
// sit in both files with byte-identical expr, for, and severity, its
// expression must contain the correcting-outcome selector and the second
// window offset, every eshu_dp_* metric it reads must be registered, and both
// copies need summary, description, and runbook annotations.
func checkDriftAuditStrikeAlerts(standalone, operator []byte, registered map[string]bool) []string {
	return checkRuleFilesForAlerts(standalone, operator, registered, driftAuditStrikeAlerts)
}

// TestDriftStrikeAlertCheckAcceptsGoodPair pins the contract's green half:
// the fixture pair the deploy PR must reproduce byte-identically (modulo
// wrapper fields) passes the check with no problems.
func TestDriftStrikeAlertCheckAcceptsGoodPair(t *testing.T) {
	t.Parallel()

	if !strings.Contains(driftStrikeStandaloneFixture, driftStrikeExpr) ||
		!strings.Contains(driftStrikeOperatorFixture, driftStrikeExpr) {
		t.Fatal("strike fixtures do not carry driftStrikeExpr verbatim")
	}
	if problems := checkDriftAuditStrikeAlerts(
		[]byte(driftStrikeStandaloneFixture), []byte(driftStrikeOperatorFixture), driftStrikeRegistered,
	); len(problems) != 0 {
		t.Fatalf("good strike pair: problems = %q, want none", strings.Join(problems, "\n"))
	}
}

// TestDriftStrikeAlertCheckFailsOnPlantedViolations proves the check has
// teeth: each planted defect in a copy of the good pair is reported.
func TestDriftStrikeAlertCheckFailsOnPlantedViolations(t *testing.T) {
	t.Parallel()

	goodStandalone, goodOperator := driftStrikeStandaloneFixture, driftStrikeOperatorFixture
	for _, tc := range []struct {
		name                 string
		standalone, operator string
		want                 string
	}{
		{
			"alert dropped from the operator file",
			goodStandalone,
			strings.ReplaceAll(goodOperator, "alert: EshuAWSDriftConsecutiveCorrectingAudits", "alert: EshuSomethingElse"),
			"operator=false",
		},
		{
			"single window without offset accepted nowhere",
			strings.Replace(goodStandalone, driftStrikeExpr, driftStrikeSingleWindowExpr, 1),
			strings.Replace(goodOperator, driftStrikeExpr, driftStrikeSingleWindowExpr, 1),
			"does not read offset 25h",
		},
		{
			"severity differs between the files",
			goodStandalone,
			strings.Replace(goodOperator, "severity: critical", "severity: warning", 1),
			"standalone and operator rules differ",
		},
		{
			"renamed metric",
			strings.ReplaceAll(goodStandalone, "eshu_dp_aws_drift_incremental_audits_total", "eshu_dp_aws_drift_incremental_audit_total"),
			goodOperator,
			"does not register",
		},
		{
			"runbook removed",
			strings.Replace(goodStandalone, "runbook: \"Inspect the delta", "runbook_missing: \"Inspect the delta", 1),
			goodOperator,
			"missing runbook",
		},
	} {
		problems := strings.Join(checkDriftAuditStrikeAlerts([]byte(tc.standalone), []byte(tc.operator), driftStrikeRegistered), "\n")
		if !strings.Contains(problems, tc.want) {
			t.Errorf("%s: problems = %q, want one containing %q", tc.name, problems, tc.want)
		}
	}
}

// TestDriftStrikeAlertsAreInBothRuleFiles is the live #11032 contract,
// modeled on TestIDAnchorCensusAlertsAreInBothRuleFiles: both deploy rule
// files at the pinned checkout carry EshuAWSDriftConsecutiveCorrectingAudits
// with driftStrikeExpr verbatim, and every eshu_dp_* metric the expression
// reads is registered by the reducer (via registeredCensusMetrics, not a
// hand-written set).
func TestDriftStrikeAlertsAreInBothRuleFiles(t *testing.T) {
	t.Parallel()

	standalone := ruleFileForContract(t, "hosted-operations-alerts.yaml")
	operator := ruleFileForContract(t, "hosted-operations-prometheus-rule.yaml")
	for name, raw := range map[string][]byte{"standalone": standalone, "operator": operator} {
		if !strings.Contains(string(raw), driftStrikeExpr) {
			t.Fatalf("%s rule file does not carry driftStrikeExpr verbatim, so the live check would prove nothing", name)
		}
	}
	for _, problem := range checkDriftAuditStrikeAlerts(standalone, operator, registeredCensusMetrics(t)) {
		t.Error(problem)
	}
}

// TestDriftStrikeAlertLiveCheckFailsOnPlantedViolation proves the live check
// has teeth against the real rule files: each planted defect in a copy of
// the pinned pair is reported.
func TestDriftStrikeAlertLiveCheckFailsOnPlantedViolation(t *testing.T) {
	t.Parallel()

	standalone := ruleFileForContract(t, "hosted-operations-alerts.yaml")
	operator := ruleFileForContract(t, "hosted-operations-prometheus-rule.yaml")
	registered := registeredCensusMetrics(t)
	for name, raw := range map[string][]byte{"standalone": standalone, "operator": operator} {
		if !strings.Contains(string(raw), driftStrikeExpr) {
			t.Fatalf("%s rule file does not carry driftStrikeExpr verbatim, so the planted clause drop would test nothing", name)
		}
	}
	for _, tc := range []struct {
		name                 string
		standalone, operator string
		want                 string
	}{
		{
			"single window without offset in both live files",
			strings.Replace(string(standalone), driftStrikeExpr, driftStrikeSingleWindowExpr, 1),
			strings.Replace(string(operator), driftStrikeExpr, driftStrikeSingleWindowExpr, 1),
			"does not read offset 25h",
		},
		{
			"alert dropped from the live operator file",
			string(standalone),
			strings.ReplaceAll(string(operator), "alert: EshuAWSDriftConsecutiveCorrectingAudits", "alert: EshuSomethingElse"),
			"operator=false",
		},
	} {
		problems := strings.Join(checkDriftAuditStrikeAlerts([]byte(tc.standalone), []byte(tc.operator), registered), "\n")
		if !strings.Contains(problems, tc.want) {
			t.Errorf("%s: problems = %q, want one containing %q", tc.name, problems, tc.want)
		}
	}
}
