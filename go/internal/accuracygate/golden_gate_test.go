// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/accuracygate"
	"github.com/eshu-hq/eshu/go/internal/admissionaudit"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// baselineFixturePath is the checked-in published accuracy baseline the gate
// enforces. Its git history is how per-dimension metrics are tracked over time.
func baselineFixturePath() string {
	return filepath.Join("testdata", "baseline.json")
}

// TestAccuracyGoldenGate is the continuous accuracy gate (issue #3499). It takes
// real measurements across the three accuracy dimensions — complexity through
// the parser, resolvers through the reducer call-edge path, correlation through
// the admission audit — and asserts each measured metric meets or exceeds its
// published baseline floor. A regression below the floor fails CI here.
func TestAccuracyGoldenGate(t *testing.T) {
	t.Parallel()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}

	baseline, err := accuracygate.LoadBaseline(baselineFixturePath())
	if err != nil {
		t.Fatalf("LoadBaseline() error = %v", err)
	}

	measurement := accuracygate.Measurement{Metrics: map[accuracygate.Dimension]accuracygate.Metric{
		accuracygate.DimensionComplexity:  measureComplexity(t, engine),
		accuracygate.DimensionResolvers:   measureResolvers(t, engine),
		accuracygate.DimensionCorrelation: measureCorrelation(t),
	}}

	result := accuracygate.Evaluate(baseline, measurement)
	if !result.Pass() {
		t.Fatalf("accuracy golden gate regressed:\n%s\n%s", result.Summary(), result.FailureMessage())
	}

	// Always emit the published metrics so a passing run still records the
	// per-dimension / per-label numbers for over-time tracking in CI logs.
	published, err := accuracygate.Publish(result).Encode()
	if err != nil {
		t.Fatalf("Publish().Encode() error = %v", err)
	}
	t.Logf("published accuracy metrics:\n%s", published)
}

// TestAccuracyGoldenGateDetectsRegression proves the gate is not a tautology: a
// fabricated measurement below the floor must fail evaluation. Without this, a
// gate that always passed would give false assurance.
func TestAccuracyGoldenGateDetectsRegression(t *testing.T) {
	t.Parallel()

	baseline, err := accuracygate.LoadBaseline(baselineFixturePath())
	if err != nil {
		t.Fatalf("LoadBaseline() error = %v", err)
	}
	regressed := accuracygate.Measurement{Metrics: map[accuracygate.Dimension]accuracygate.Metric{
		accuracygate.DimensionComplexity:  {Precision: 0, Recall: 0, CoveredItems: 0},
		accuracygate.DimensionResolvers:   {Precision: 0, Recall: 0, CoveredItems: 0},
		accuracygate.DimensionCorrelation: {Precision: 0, Recall: 0, CoveredItems: 0},
	}}
	result := accuracygate.Evaluate(baseline, regressed)
	if result.Pass() {
		t.Fatalf("gate passed a fully-regressed measurement: %s", result.Summary())
	}
	for _, dimension := range []string{"complexity", "resolvers", "correlation"} {
		if !strings.Contains(result.FailureMessage(), dimension) {
			t.Fatalf("regression message missing %q: %q", dimension, result.FailureMessage())
		}
	}
}

// TestAccuracyGoldenGateDetectsCorrelationRegression proves the correlation
// dimension observes real production admission behavior, not the golden
// expectation. It drops the required deployment-repository evidence from the
// admitted service case's INPUT, so the real correlation/admission.Evaluate
// rejects what the golden suite expects admitted. That flipped production
// decision must make the audit report a disagreement, the correlation metric
// fall below its floor, and the gate fail. A tautological measurement that copied
// ExpectedState would still report a perfect score here.
func TestAccuracyGoldenGateDetectsCorrelationRegression(t *testing.T) {
	t.Parallel()

	baseline, err := accuracygate.LoadBaseline(baselineFixturePath())
	if err != nil {
		t.Fatalf("LoadBaseline() error = %v", err)
	}

	suite, err := admissionaudit.LoadSuite(correlationGoldenPath())
	if err != nil {
		t.Fatalf("LoadSuite() error = %v", err)
	}

	// Healthy inputs score a passing correlation metric: the baseline guard
	// confirms the regression below is what fails the gate, not a bad fixture.
	healthy := scoreCorrelationInputs(t, suite.Intents, correlationInputs())
	if pass, reason := dimensionPasses(baseline, accuracygate.DimensionCorrelation, healthy); !pass {
		t.Fatalf("healthy correlation inputs failed the floor: %s", reason)
	}

	regressed := correlationInputs()
	admitted := regressed["deployable-service-admitted"]
	// Strip the deployment-repository evidence the production structure gate
	// requires; admission.Evaluate now rejects the candidate the golden expects
	// admitted, exactly the regression a real correlation defect would cause.
	admitted.candidate.Evidence = nil
	regressed["deployable-service-admitted"] = admitted

	metric := scoreCorrelationInputs(t, suite.Intents, regressed)
	if pass, _ := dimensionPasses(baseline, accuracygate.DimensionCorrelation, metric); pass {
		t.Fatalf("correlation gate passed a regressed admission decision: %+v", metric)
	}
}

// dimensionPasses evaluates a single dimension's measured metric against the
// baseline and returns whether it cleared the floor plus the failure reason.
func dimensionPasses(
	baseline accuracygate.Baseline,
	dimension accuracygate.Dimension,
	metric accuracygate.Metric,
) (bool, string) {
	result := accuracygate.Evaluate(baseline, accuracygate.Measurement{
		Metrics: map[accuracygate.Dimension]accuracygate.Metric{dimension: metric},
	})
	for _, dimResult := range result.Results {
		if dimResult.Dimension == dimension {
			return dimResult.Pass, dimResult.Reason
		}
	}
	return false, "dimension not evaluated"
}

// TestAccuracyGoldenGateDetectsResolverCoverageRegression proves the resolver
// dimension's CoveredItems is MEASURED, not a documented constant. It runs the
// real per-language extraction over the coverage fixtures, then removes one
// resolver's firing signal from its fixture so that resolver no longer produces
// its CALLS edge — exactly what removing the resolver would do. The measured
// covered count must drop below the floor and the gate fail. A count derived from
// a hard-coded README list would stay at the floor and pass.
func TestAccuracyGoldenGateDetectsResolverCoverageRegression(t *testing.T) {
	t.Parallel()

	baseline, err := accuracygate.LoadBaseline(baselineFixturePath())
	if err != nil {
		t.Fatalf("LoadBaseline() error = %v", err)
	}

	// Healthy: every documented resolver fires, so the measured count clears the
	// floor. This guards that the regression below is what drops coverage.
	healthy := measureResolverCoverage(t)
	if got := countCoveredResolvers(healthy); got < baseline.Thresholds[accuracygate.DimensionResolvers].MinCoveredItems {
		t.Fatalf("healthy resolver coverage = %d, below floor %d", got, baseline.Thresholds[accuracygate.DimensionResolvers].MinCoveredItems)
	}

	// Drop one resolver's firing signal: a Kotlin call with no imported receiver
	// type cannot bind through the Kotlin imported-receiver resolver, so its edge
	// disappears — the same coverage loss removing the resolver would cause.
	regressed := measureResolverCoverageWithBrokenLanguage(t, "kotlin")
	covered := countCoveredResolvers(regressed)
	floor := baseline.Thresholds[accuracygate.DimensionResolvers].MinCoveredItems
	if covered >= floor {
		t.Fatalf("broken kotlin resolver still covered=%d at/above floor %d", covered, floor)
	}

	metric := accuracygate.Metric{Precision: 1, Recall: 1, CoveredItems: covered}
	result := accuracygate.Evaluate(baseline, accuracygate.Measurement{
		Metrics: map[accuracygate.Dimension]accuracygate.Metric{accuracygate.DimensionResolvers: metric},
	})
	if result.Pass() {
		t.Fatalf("resolver gate passed with a dropped resolver: covered=%d floor=%d", covered, floor)
	}
}

// TestAccuracyResolverMatrixMatchesPublishedDoc keeps the gate's resolver
// coverage set in lockstep with the published #3487 matrix in the reducer
// README, so the coverage count the gate enforces cannot silently drift from the
// documented coverage.
func TestAccuracyResolverMatrixMatchesPublishedDoc(t *testing.T) {
	t.Parallel()

	readmePath := filepath.Join("..", "reducer", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read reducer README error = %v", err)
	}
	documented := documentedResolverLanguages(string(data))

	gate := append([]string(nil), resolverCoveredLanguages()...)
	sort.Strings(gate)
	sort.Strings(documented)

	if strings.Join(gate, ",") != strings.Join(documented, ",") {
		t.Fatalf("resolver coverage drift:\n  gate      = %v\n  documented= %v", gate, documented)
	}
}

// documentedResolverLanguages parses the reducer README coverage matrix rows and
// returns the languages whose "Dedicated resolver" column is "yes", splitting
// combined cells such as "typescript / tsx" and "javascript / jsx".
func documentedResolverLanguages(readme string) []string {
	seen := make(map[string]struct{})
	for _, line := range strings.Split(readme, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) < 4 {
			continue
		}
		name := strings.TrimSpace(columns[1])
		dedicated := strings.TrimSpace(columns[2])
		if dedicated != "yes" {
			continue
		}
		for _, part := range strings.Split(name, "/") {
			language := strings.TrimSpace(part)
			if language == "" {
				continue
			}
			seen[language] = struct{}{}
		}
	}
	languages := make([]string, 0, len(seen))
	for language := range seen {
		languages = append(languages, language)
	}
	return languages
}
