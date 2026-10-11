// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package census

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"gopkg.in/yaml.v3"
)

// The id-anchor census alerts (#7212). Since PR-B, a nonzero unreachable gauge
// means entity ids that GET /api/v0/entities/{entity_id}/context answers 404
// on Neo4j, so the gauge pages. The standalone rule file and the Prometheus
// Operator wrapper must carry the same alerts (deploy/observability/README.md).
const (
	standaloneAlertsPath = "../../../../../deploy/observability/alerts.yaml"
	operatorRulePath     = "../../../../../deploy/observability/id-anchor-census-prometheus-rule.yaml"
)

// idAnchorCensusAlerts are the alerts both rule files must carry, with the
// metric selectors each expression must contain. The stale alert reads the
// pass counter as well as the last-success gauge: a reducer whose census never
// succeeds after a restart never exports the gauge, so the gauge clause alone
// returns no data and cannot fire (review round 2, G1).
var idAnchorCensusAlerts = map[string][]string{
	"EshuGraphIDAnchorUnreachableNodes": {
		"eshu_dp_graph_id_anchor_unreachable_nodes",
		"eshu_dp_graph_id_anchor_id_bearing_nodes",
	},
	"EshuGraphIDAnchorCensusStale": {
		"eshu_dp_graph_id_anchor_census_last_success_unixtime",
		`eshu_dp_graph_id_anchor_census_passes_total{outcome="failed"}`,
		`eshu_dp_graph_id_anchor_census_passes_total{outcome="ok"}`,
	},
}

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type ruleGroup struct {
	Name  string      `yaml:"name"`
	Rules []alertRule `yaml:"rules"`
}

// parseRuleGroups reads either a standalone Prometheus rule file (top-level
// groups) or a PrometheusRule object (spec.groups).
func parseRuleGroups(raw []byte) ([]ruleGroup, error) {
	var doc struct {
		Kind   string      `yaml:"kind"`
		Groups []ruleGroup `yaml:"groups"`
		Spec   struct {
			Groups []ruleGroup `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == "PrometheusRule" {
		return doc.Spec.Groups, nil
	}
	return doc.Groups, nil
}

// censusStaleExpr is the stale alert's expression as both rule files write it,
// and censusStaleGaugeOnlyExpr the gauge-only form that cannot fire when no
// pass has succeeded since the reducer started. The planted-violation test
// swaps one for the other.
const (
	censusStaleExpr          = `(time() - max(eshu_dp_graph_id_anchor_census_last_success_unixtime) > 10800) or (sum(increase(eshu_dp_graph_id_anchor_census_passes_total{outcome="failed"}[3h])) > 0 unless sum(increase(eshu_dp_graph_id_anchor_census_passes_total{outcome="ok"}[3h])) > 0)`
	censusStaleGaugeOnlyExpr = `time() - max(eshu_dp_graph_id_anchor_census_last_success_unixtime) > 10800`
)

var metricNameRe = regexp.MustCompile(`\beshu_dp_[a-z0-9_]+\b`)

// checkIDAnchorCensusAlerts returns every way the two rule files fail the
// contract: an alert missing from either file, an expression, duration or
// severity that differs between them, an expression that omits a required
// metric or reads a metric the reducer does not register, or an alert without
// a summary, description and runbook.
func checkIDAnchorCensusAlerts(standalone, operator []byte, registered map[string]bool) []string {
	var problems []string
	byFile := map[string]map[string]alertRule{}
	for name, raw := range map[string][]byte{"standalone": standalone, "operator": operator} {
		groups, err := parseRuleGroups(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: parse: %v", name, err))
			continue
		}
		rules := map[string]alertRule{}
		for _, group := range groups {
			for _, rule := range group.Rules {
				if _, ok := idAnchorCensusAlerts[rule.Alert]; ok {
					rules[rule.Alert] = rule
				}
			}
		}
		byFile[name] = rules
	}
	for alert, metrics := range idAnchorCensusAlerts {
		standaloneRule, inStandalone := byFile["standalone"][alert]
		operatorRule, inOperator := byFile["operator"][alert]
		if !inStandalone || !inOperator {
			problems = append(problems, fmt.Sprintf("%s: standalone=%v operator=%v, want both", alert, inStandalone, inOperator))
			continue
		}
		if strings.TrimSpace(standaloneRule.Expr) != strings.TrimSpace(operatorRule.Expr) ||
			standaloneRule.For != operatorRule.For ||
			standaloneRule.Labels["severity"] != operatorRule.Labels["severity"] {
			problems = append(problems, fmt.Sprintf("%s: the standalone and operator rules differ", alert))
		}
		for _, metric := range metrics {
			if !strings.Contains(standaloneRule.Expr, metric) {
				problems = append(problems, fmt.Sprintf("%s: expression does not read %s", alert, metric))
			}
		}
		for _, metric := range metricNameRe.FindAllString(standaloneRule.Expr, -1) {
			if !registered[metric] {
				problems = append(problems, fmt.Sprintf("%s: expression reads %s, which the reducer does not register", alert, metric))
			}
		}
		for _, key := range []string{"summary", "description", "runbook"} {
			if strings.TrimSpace(standaloneRule.Annotations[key]) == "" || strings.TrimSpace(operatorRule.Annotations[key]) == "" {
				problems = append(problems, fmt.Sprintf("%s: missing %s annotation", alert, key))
			}
		}
	}
	return problems
}

// registeredCensusMetrics returns the census metric names the telemetry
// package actually exports, by recording each census instrument once and
// collecting, rather than by copying the names.
func registeredCensusMetrics(t *testing.T) map[string]bool {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	inst, err := telemetry.NewInstruments(provider.Meter("census-alert-rule-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	ctx := context.Background()
	inst.GraphIDAnchorUnreachableNodes.Record(ctx, 0)
	inst.GraphIDAnchorIDBearingNodes.Record(ctx, 1)
	inst.GraphIDAnchorCensusLastSuccess.Record(ctx, 1)
	inst.GraphIDAnchorCensusPasses.Add(ctx, 1)
	inst.GraphIDAnchorCensusDuration.Record(ctx, 1)
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	names := map[string]bool{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			names[m.Name] = true
		}
	}
	return names
}

func TestIDAnchorCensusAlertsAreInBothRuleFiles(t *testing.T) {
	t.Parallel()

	standalone, err := os.ReadFile(standaloneAlertsPath)
	if err != nil {
		t.Fatalf("read %s: %v", standaloneAlertsPath, err)
	}
	operator, err := os.ReadFile(operatorRulePath)
	if err != nil {
		t.Fatalf("read %s: %v", operatorRulePath, err)
	}
	for _, problem := range checkIDAnchorCensusAlerts(standalone, operator, registeredCensusMetrics(t)) {
		t.Error(problem)
	}
}

// TestIDAnchorCensusAlertCheckFailsOnPlantedViolations proves the check above
// has teeth: each planted defect in a copy of the real rule files is reported.
func TestIDAnchorCensusAlertCheckFailsOnPlantedViolations(t *testing.T) {
	t.Parallel()

	standalone, err := os.ReadFile(standaloneAlertsPath)
	if err != nil {
		t.Fatalf("read %s: %v", standaloneAlertsPath, err)
	}
	operator, err := os.ReadFile(operatorRulePath)
	if err != nil {
		t.Fatalf("read %s: %v", operatorRulePath, err)
	}
	registered := registeredCensusMetrics(t)
	for name, raw := range map[string][]byte{"standalone": standalone, "operator": operator} {
		if !strings.Contains(string(raw), censusStaleExpr) {
			t.Fatalf("%s rule file does not carry censusStaleExpr verbatim, so the planted clause drop would test nothing", name)
		}
	}
	for _, tc := range []struct {
		name                 string
		standalone, operator string
		want                 string
	}{
		{
			"alert dropped from the operator file",
			string(standalone), strings.ReplaceAll(string(operator), "alert: EshuGraphIDAnchorUnreachableNodes", "alert: EshuSomethingElse"),
			"operator=false",
		},
		{
			"threshold differs between the files",
			string(standalone), strings.Replace(string(operator), "eshu_dp_graph_id_anchor_unreachable_nodes) > 0", "eshu_dp_graph_id_anchor_unreachable_nodes) > 10", 1),
			"differ",
		},
		{
			"empty-graph guard removed",
			strings.Replace(string(standalone), "eshu_dp_graph_id_anchor_id_bearing_nodes", "eshu_dp_graph_id_anchor_unreachable_nodes", 1), string(operator),
			"does not read eshu_dp_graph_id_anchor_id_bearing_nodes",
		},
		{
			"renamed metric",
			strings.ReplaceAll(string(standalone), "eshu_dp_graph_id_anchor_census_last_success_unixtime", "eshu_dp_graph_id_anchor_census_last_ok_unixtime"), string(operator),
			"does not register",
		},
		{
			"failed-without-success clause dropped from both files",
			strings.Replace(string(standalone), censusStaleExpr, censusStaleGaugeOnlyExpr, 1),
			strings.Replace(string(operator), censusStaleExpr, censusStaleGaugeOnlyExpr, 1),
			`does not read eshu_dp_graph_id_anchor_census_passes_total{outcome="failed"}`,
		},
		{
			"runbook removed",
			strings.Replace(string(standalone), "runbook: |\n            The census found", "runbook_missing: |\n            The census found", 1), string(operator),
			"missing runbook",
		},
	} {
		problems := strings.Join(checkIDAnchorCensusAlerts([]byte(tc.standalone), []byte(tc.operator), registered), "\n")
		if !strings.Contains(problems, tc.want) {
			t.Errorf("%s: problems = %q, want one containing %q", tc.name, problems, tc.want)
		}
	}
}
