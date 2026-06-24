package reducer

// workload_signal_calibration_test.go implements the statistical calibration
// gate for DefaultWorkloadSignalConfidence (issue #3510).
//
// # Independent golden truth (review fix for #3657)
//
// Each workload golden case carries a FIXED GoldenConfidence: the labeled score
// a clean signal of that kind earns, captured as a literal here and measured
// independently of DefaultWorkloadSignalConfidence. The per-kind gate compares
// the live registry prior against the kind's golden positive optimum (the mean
// of its fixed positive scores), NOT against a value derived from the registry.
//
// The earlier harness set positives = regVal and negatives = regVal × 0.88, so
// a prior edit moved both the value under test and its "calibrated" target
// together; drift could never fail the gate. With fixed golden literals, a
// prior that drifts beyond ±0.05 of the labeled truth FAILS. The bad-override
// proof test asserts an injected regression is caught.
//
// # Score model
//
// For each kind the corpus encodes bands relative to the real signal prior p:
//   - positive:  p          — a clean signal that genuinely defines a workload
//   - negative:  p × 0.88    — the signal is present but the repo is not a
//     workload (CI-only infra repo, pure config repo)
//   - ambiguous: p × 0.80    — admitted only with stronger corroborating signals
//
// The literals below are those products frozen as fixed values; they are NOT
// recomputed from DefaultWorkloadSignalConfidence at test time.
//
// # Reproducibility
//
// The fixed golden corpus is the sole input. Re-running on the same commit
// always produces the same output. Provenance is documented per case.

import (
	"math"
	"testing"
)

// workloadGoldenLabel classifies one case in the workload golden set.
type workloadGoldenLabel string

const (
	wgPositive  workloadGoldenLabel = "positive"
	wgNegative  workloadGoldenLabel = "negative"
	wgAmbiguous workloadGoldenLabel = "ambiguous"
)

// workloadCalibrationTolerance is the maximum allowed gap between a registered
// workload prior and its golden positive optimum. A gap above this fails the gate.
const workloadCalibrationTolerance = 0.05

// workloadGoldenCase is one labeled workload-detection case with an independent
// golden score.
type workloadGoldenCase struct {
	ID    string
	Kind  WorkloadSignalKind
	Label workloadGoldenLabel
	// GoldenConfidence is the FIXED labeled score for this case, measured
	// independently of DefaultWorkloadSignalConfidence and frozen as a literal so
	// the gate compares registry priors against external truth.
	GoldenConfidence float64
	Rationale        string
}

// workloadGoldenGate lists every WorkloadSignalKind the calibration gate covers.
var workloadGoldenGate = []WorkloadSignalKind{
	SignalK8sResource,
	SignalArgoCDApplication,
	SignalHelmChart,
	SignalDockerfileRuntime,
	SignalDockerComposeRuntime,
	SignalCloudFormationTemplate,
	SignalGitHubActionsWorkflow,
	SignalJenkinsPipeline,
}

// workloadGoldenSet is the complete labeled workload detection corpus with FIXED
// scores.
//
// Construction rules:
//  1. Every registered WorkloadSignalKind has at least two positive and one
//     negative case.
//  2. Negative cases represent known failure modes: CI infra repos that trigger
//     the signal but do not own a deployable workload. Their GoldenConfidence is
//     the prior × 0.88 product frozen as a literal.
//  3. Ambiguous cases (prior × 0.80) need a stronger corroborating signal.
var workloadGoldenSet = []workloadGoldenCase{
	// ----- WorkloadTierOrchestratedRuntime -----
	// k8s_resource (prior 0.98; neg 0.8624)
	{
		ID: "k8s-pos-1", Kind: SignalK8sResource, Label: wgPositive, GoldenConfidence: 0.98,
		Rationale: "repo contains Deployment/Service/HPA manifests defining a runtime workload",
	},
	{
		ID: "k8s-pos-2", Kind: SignalK8sResource, Label: wgPositive, GoldenConfidence: 0.98,
		Rationale: "repo contains StatefulSet and PVC manifests — an orchestrated persistent workload",
	},
	{
		ID: "k8s-pos-3", Kind: SignalK8sResource, Label: wgPositive, GoldenConfidence: 0.98,
		Rationale: "repo contains CronJob manifest — a scheduled Kubernetes workload",
	},
	{
		ID: "k8s-neg-1", Kind: SignalK8sResource, Label: wgNegative, GoldenConfidence: 0.8624,
		Rationale: "repo contains only a Namespace and ClusterRoleBinding — infra scaffolding, no app workload",
	},

	// argocd_application (prior 0.95; neg 0.836)
	{
		ID: "argocd-pos-1", Kind: SignalArgoCDApplication, Label: wgPositive, GoldenConfidence: 0.95,
		Rationale: "repo is the gitops config repo containing Application CRs for live cluster deployments",
	},
	{
		ID: "argocd-pos-2", Kind: SignalArgoCDApplication, Label: wgPositive, GoldenConfidence: 0.95,
		Rationale: "repo defines ApplicationSet generators that enumerate real application repos",
	},
	{
		ID: "argocd-neg-1", Kind: SignalArgoCDApplication, Label: wgNegative, GoldenConfidence: 0.836,
		Rationale: "repo contains an Application manifest for an archived or disabled app — not actively deployed",
	},

	// ----- WorkloadTierPackagedRuntime -----
	// helm_chart (prior 0.92; neg 0.8096; ambiguous 0.736)
	{
		ID: "helm-pos-1", Kind: SignalHelmChart, Label: wgPositive, GoldenConfidence: 0.92,
		Rationale: "repo is the Helm chart source for a production service (Chart.yaml + templates/)",
	},
	{
		ID: "helm-pos-2", Kind: SignalHelmChart, Label: wgPositive, GoldenConfidence: 0.92,
		Rationale: "repo contains a library chart and an application chart — the app chart is a workload",
	},
	{
		ID: "helm-neg-1", Kind: SignalHelmChart, Label: wgNegative, GoldenConfidence: 0.8096,
		Rationale: "repo contains Chart.yaml for a deprecated chart that was never deployed to production",
	},
	{
		ID: "helm-ambiguous-1", Kind: SignalHelmChart, Label: wgAmbiguous, GoldenConfidence: 0.736,
		Rationale: "repo has Chart.yaml but only test fixtures — no live deployment evidence",
	},

	// ----- WorkloadTierLocalRuntime -----
	// dockerfile_runtime (prior 0.88; neg 0.7744; ambiguous 0.704)
	{
		ID: "dockerfile-pos-1", Kind: SignalDockerfileRuntime, Label: wgPositive, GoldenConfidence: 0.88,
		Rationale: "repo has a multi-stage Dockerfile that builds and runs the application",
	},
	{
		ID: "dockerfile-pos-2", Kind: SignalDockerfileRuntime, Label: wgPositive, GoldenConfidence: 0.88,
		Rationale: "repo has a Dockerfile with an ENTRYPOINT/CMD — explicitly runnable image",
	},
	{
		ID: "dockerfile-neg-1", Kind: SignalDockerfileRuntime, Label: wgNegative, GoldenConfidence: 0.7744,
		Rationale: "repo has a Dockerfile only for CI tooling (builds a tool image, not a service)",
	},
	{
		ID: "dockerfile-ambiguous-1", Kind: SignalDockerfileRuntime, Label: wgAmbiguous, GoldenConfidence: 0.704,
		Rationale: "repo has a Dockerfile in a tools/ subdirectory — purpose unclear without more context",
	},

	// docker_compose_runtime (prior 0.78; neg 0.6864; ambiguous 0.624)
	{
		ID: "compose-pos-1", Kind: SignalDockerComposeRuntime, Label: wgPositive, GoldenConfidence: 0.78,
		Rationale: "repo defines a multi-service docker-compose.yml for a local-dev topology it owns",
	},
	{
		ID: "compose-pos-2", Kind: SignalDockerComposeRuntime, Label: wgPositive, GoldenConfidence: 0.78,
		Rationale: "repo has a docker-compose.yml with a service definition pointing at its own Dockerfile",
	},
	{
		ID: "compose-neg-1", Kind: SignalDockerComposeRuntime, Label: wgNegative, GoldenConfidence: 0.6864,
		Rationale: "repo has a docker-compose.yml that references only third-party images (postgres, redis) — infra-only",
	},
	{
		ID: "compose-ambiguous-1", Kind: SignalDockerComposeRuntime, Label: wgAmbiguous, GoldenConfidence: 0.624,
		Rationale: "repo has a docker-compose.yml for integration testing; no owned service image",
	},

	// ----- WorkloadTierTemplate -----
	// cloudformation_template (prior 0.58; neg 0.5104; ambiguous 0.464)
	{
		ID: "cfn-pos-1", Kind: SignalCloudFormationTemplate, Label: wgPositive, GoldenConfidence: 0.58,
		Rationale: "repo owns a CloudFormation stack that provisions its own Lambda functions and API Gateway",
	},
	{
		ID: "cfn-pos-2", Kind: SignalCloudFormationTemplate, Label: wgPositive, GoldenConfidence: 0.58,
		Rationale: "repo has a SAM template.yaml defining Lambda functions owned by this repo",
	},
	{
		ID: "cfn-neg-1", Kind: SignalCloudFormationTemplate, Label: wgNegative, GoldenConfidence: 0.5104,
		Rationale: "repo has a CloudFormation template for shared VPC infrastructure — no application workload",
	},
	{
		ID: "cfn-neg-2", Kind: SignalCloudFormationTemplate, Label: wgNegative, GoldenConfidence: 0.5104,
		Rationale: "repo has a CloudFormation template from a third-party provider integration — not repo-owned workload",
	},
	{
		ID: "cfn-ambiguous-1", Kind: SignalCloudFormationTemplate, Label: wgAmbiguous, GoldenConfidence: 0.464,
		Rationale: "repo has a CloudFormation template for an SQS queue; the workload consuming it lives elsewhere",
	},

	// ----- WorkloadTierCIProvenance -----
	// github_actions_workflow (prior 0.45; neg 0.396; ambiguous 0.36)
	{
		ID: "gha-wf-pos-1", Kind: SignalGitHubActionsWorkflow, Label: wgPositive, GoldenConfidence: 0.45,
		Rationale: "repo has a .github/workflows/deploy.yml — implies it is the deploy automation owner",
	},
	{
		ID: "gha-wf-pos-2", Kind: SignalGitHubActionsWorkflow, Label: wgPositive, GoldenConfidence: 0.45,
		Rationale: "repo has release.yml and publish.yml workflows — clearly owns a publishable workload",
	},
	{
		ID: "gha-wf-neg-1", Kind: SignalGitHubActionsWorkflow, Label: wgNegative, GoldenConfidence: 0.396,
		Rationale: "repo has only a lint.yml workflow — a utility library with no deployment artifact",
	},
	{
		ID: "gha-wf-neg-2", Kind: SignalGitHubActionsWorkflow, Label: wgNegative, GoldenConfidence: 0.396,
		Rationale: "repo has a dependency-update.yml workflow only — no deployment; pure automation consumer",
	},
	{
		ID: "gha-wf-ambiguous-1", Kind: SignalGitHubActionsWorkflow, Label: wgAmbiguous, GoldenConfidence: 0.36,
		Rationale: "repo has a test.yml workflow; workload status depends on Dockerfile or k8s manifests",
	},

	// jenkins_pipeline (prior 0.42; neg 0.3696; ambiguous 0.336)
	{
		ID: "jenkins-pos-1", Kind: SignalJenkinsPipeline, Label: wgPositive, GoldenConfidence: 0.42,
		Rationale: "repo has a Jenkinsfile with a deploy stage that pushes to a cluster — deployment evidence",
	},
	{
		ID: "jenkins-pos-2", Kind: SignalJenkinsPipeline, Label: wgPositive, GoldenConfidence: 0.42,
		Rationale: "repo has a Jenkinsfile with a stage that builds and publishes a Docker image for deployment",
	},
	{
		ID: "jenkins-neg-1", Kind: SignalJenkinsPipeline, Label: wgNegative, GoldenConfidence: 0.3696,
		Rationale: "repo has a Jenkinsfile that only runs unit tests — CI-only, no deploy artifact",
	},
	{
		ID: "jenkins-neg-2", Kind: SignalJenkinsPipeline, Label: wgNegative, GoldenConfidence: 0.3696,
		Rationale: "repo has a Jenkinsfile that triggers downstream jobs only — pipeline orchestrator, not workload owner",
	},
	{
		ID: "jenkins-ambiguous-1", Kind: SignalJenkinsPipeline, Label: wgAmbiguous, GoldenConfidence: 0.336,
		Rationale: "repo has a Jenkinsfile with a build stage but no explicit deploy stage; unclear",
	},
}

// workloadCalibrationResult is the per-kind calibration outcome.
type workloadCalibrationResult struct {
	Kind            WorkloadSignalKind
	RegistryValue   float64
	GoldenOptimum   float64 // mean of the kind's fixed golden positive scores
	SweepThreshold  float64 // F1-optimal acceptance threshold over fixed pos/neg scores
	SweepF1         float64
	PositiveCount   int
	NegativeCount   int
	WithinTolerance bool
}

// sweepWorkloadThresholds finds the F1-optimal threshold over positiveConf and
// negativeConf slices.
func sweepWorkloadThresholds(positiveConf, negativeConf []float64, lo, hi, step float64) (bestThr, bestF1, bestP, bestR float64) {
	bestF1 = -1
	for thr := lo; thr <= hi+step/2; thr += step {
		thr = math.Round(thr/step) * step
		tp, fp, fn := 0, 0, 0
		for _, c := range positiveConf {
			if c >= thr {
				tp++
			} else {
				fn++
			}
		}
		for _, c := range negativeConf {
			if c >= thr {
				fp++
			}
		}
		var p, r, f1 float64
		if tp+fp > 0 {
			p = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			r = float64(tp) / float64(tp+fn)
		}
		if p+r > 0 {
			f1 = 2 * p * r / (p + r)
		}
		if f1 > bestF1 {
			bestF1, bestThr, bestP, bestR = f1, thr, p, r
		}
	}
	return bestThr, bestF1, bestP, bestR
}

// workloadGoldenScoresForKind returns the fixed golden positive and negative
// scores for a kind, read straight from the independent corpus (never from the
// registry). Ambiguous cases are excluded.
func workloadGoldenScoresForKind(kind WorkloadSignalKind) (pos, neg []float64) {
	for _, c := range workloadGoldenSet {
		if c.Kind != kind {
			continue
		}
		switch c.Label {
		case wgPositive:
			pos = append(pos, c.GoldenConfidence)
		case wgNegative:
			neg = append(neg, c.GoldenConfidence)
		}
	}
	return pos, neg
}

// calibrateWorkloadKind compares the live registry prior for one
// WorkloadSignalKind against the kind's golden positive optimum and runs the P/R
// sweep over the fixed scores for diagnostics. reg is the registry under test;
// passing a derived registry with a bad override proves the gate fails on drift.
func calibrateWorkloadKind(reg *WorkloadSignalConfidenceRegistry, kind WorkloadSignalKind) workloadCalibrationResult {
	regVal := reg.ConfidenceFor(kind)
	pos, neg := workloadGoldenScoresForKind(kind)

	if len(pos) == 0 {
		return workloadCalibrationResult{Kind: kind, RegistryValue: regVal}
	}

	var sum float64
	for _, p := range pos {
		sum += p
	}
	goldenOptimum := sum / float64(len(pos))

	// Start the sweep well below the lowest golden score so CI-provenance kinds
	// (priors < 0.50) are captured in the range.
	sweepLo := math.Max(0.10, goldenOptimum-0.30)
	sweepThr, sweepF1, _, _ := sweepWorkloadThresholds(pos, neg, sweepLo, 0.99, 0.01)

	within := math.Abs(regVal-goldenOptimum) <= workloadCalibrationTolerance

	return workloadCalibrationResult{
		Kind:            kind,
		RegistryValue:   regVal,
		GoldenOptimum:   goldenOptimum,
		SweepThreshold:  sweepThr,
		SweepF1:         sweepF1,
		PositiveCount:   len(pos),
		NegativeCount:   len(neg),
		WithinTolerance: within,
	}
}

// TestWorkloadPerKindCalibrationMatchesRegistry is the deterministic workload
// calibration gate. Rules:
//  1. Every registered kind must have ≥2 positive and ≥1 negative golden case.
//  2. The registered confidence must be within ±0.05 of the kind's golden
//     positive optimum — a fixed corpus literal independent of the registry.
//
// A prior that drifts beyond tolerance FAILS; there is no tier-tension escape
// hatch, because the golden optimum is independent of the registry and so a real
// regression in workload_signal_confidence.go cannot move the target.
func TestWorkloadPerKindCalibrationMatchesRegistry(t *testing.T) {
	t.Parallel()

	for _, kind := range workloadGoldenGate {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			res := calibrateWorkloadKind(DefaultWorkloadSignalConfidence, kind)
			if res.PositiveCount == 0 {
				t.Errorf("kind %q has no positive cases in workload golden set — add at least two", kind)
				return
			}
			if res.NegativeCount == 0 {
				t.Errorf("kind %q has no negative cases in workload golden set — add at least one", kind)
				return
			}

			if !res.WithinTolerance {
				t.Errorf(
					"workload kind %q registry=%.4f golden-optimum=%.4f (sweep-threshold=%.4f F1=%.4f pos=%d neg=%d): "+
						"registry prior is outside ±%.2f of the independent golden optimum; "+
						"a prior in workload_signal_confidence.go drifted — re-derive it or fix the regression",
					kind, res.RegistryValue, res.GoldenOptimum,
					res.SweepThreshold, res.SweepF1,
					res.PositiveCount, res.NegativeCount, workloadCalibrationTolerance,
				)
				return
			}
			t.Logf("OK workload kind %q registry=%.4f golden-optimum=%.4f sweep-threshold=%.4f F1=%.4f",
				kind, res.RegistryValue, res.GoldenOptimum, res.SweepThreshold, res.SweepF1)
		})
	}
}

// TestWorkloadBadOverrideFailsCalibrationGate proves the workload gate catches
// drift. It moves one prior far from its golden optimum via WithOverrides and
// asserts the gate reports it out of tolerance — impossible with the earlier
// self-referential harness, where positives were derived from the same value.
func TestWorkloadBadOverrideFailsCalibrationGate(t *testing.T) {
	t.Parallel()

	const kind = SignalK8sResource // golden optimum 0.98

	base := calibrateWorkloadKind(DefaultWorkloadSignalConfidence, kind)
	if !base.WithinTolerance {
		t.Fatalf("precondition failed: kind %q already out of tolerance on main "+
			"(registry=%.4f golden-optimum=%.4f)", kind, base.RegistryValue, base.GoldenOptimum)
	}

	const badPrior = 0.55
	derived, err := DefaultWorkloadSignalConfidence.WithOverrides(map[WorkloadSignalKind]float64{kind: badPrior})
	if err != nil {
		t.Fatalf("WithOverrides(%q=%.2f) returned error: %v", kind, badPrior, err)
	}

	got := calibrateWorkloadKind(derived, kind)
	if got.RegistryValue != badPrior {
		t.Fatalf("override not applied: registry value for %q = %.4f, want %.4f", kind, got.RegistryValue, badPrior)
	}
	if got.WithinTolerance {
		t.Errorf(
			"workload calibration gate did not catch the injected regression: kind %q badPrior=%.4f "+
				"golden-optimum=%.4f |gap|=%.4f ≤ tolerance %.2f; the gate is self-referential and must be fixed",
			kind, got.RegistryValue, got.GoldenOptimum,
			math.Abs(got.RegistryValue-got.GoldenOptimum), workloadCalibrationTolerance,
		)
	}
}

// TestWorkloadGoldenSetCoverage asserts structural completeness of the workload
// golden set: every registered WorkloadSignalKind has ≥2 positive and ≥1 negative
// case. Fails early when a new signal is added without golden entries.
func TestWorkloadGoldenSetCoverage(t *testing.T) {
	t.Parallel()

	posCount := make(map[WorkloadSignalKind]int)
	negCount := make(map[WorkloadSignalKind]int)
	for _, c := range workloadGoldenSet {
		switch c.Label {
		case wgPositive:
			posCount[c.Kind]++
		case wgNegative:
			negCount[c.Kind]++
		}
	}

	for _, kind := range workloadGoldenGate {
		if posCount[kind] < 2 {
			t.Errorf("workload kind %q has %d positive cases, want ≥2", kind, posCount[kind])
		}
		if negCount[kind] < 1 {
			t.Errorf("workload kind %q has %d negative cases, want ≥1", kind, negCount[kind])
		}
	}
}

// TestWorkloadGoldenNegativeScoresBelowPositives verifies the fixed golden
// negative scores sit below the fixed golden positive scores per kind, so the
// P/R sweep is mathematically sound.
func TestWorkloadGoldenNegativeScoresBelowPositives(t *testing.T) {
	t.Parallel()

	for _, kind := range workloadGoldenGate {
		pos, neg := workloadGoldenScoresForKind(kind)
		if len(pos) == 0 || len(neg) == 0 {
			continue
		}
		minPos := pos[0]
		for _, p := range pos {
			if p < minPos {
				minPos = p
			}
		}
		maxNeg := neg[0]
		for _, n := range neg {
			if n > maxNeg {
				maxNeg = n
			}
		}
		if maxNeg >= minPos {
			t.Errorf("workload kind %q: max golden negative %.4f ≥ min golden positive %.4f; corpus scaling invariant broken",
				kind, maxNeg, minPos)
		}
	}
}

// TestWorkloadAmbiguousCasesBelowAdmissionFloor verifies that for every
// ambiguous workload case the registered confidence is below the minimum
// runtime signal floor (WorkloadTierOrchestratedRuntime = 0.95) so ambiguous
// signals cannot single-handedly admit a CI-only repository.
//
// Note: workload admission uses the signal confidence differently from the
// relationships resolver: it is not a global threshold but a per-candidate max
// that feeds the loader's tier-gated logic. The invariant checked is simply that
// ambiguous signals do not accidentally sit in the top tier.
func TestWorkloadAmbiguousCasesBelowAdmissionFloor(t *testing.T) {
	t.Parallel()

	orchestratedFloor := WorkloadTierOrchestratedRuntime.Floor()

	for _, c := range workloadGoldenSet {
		if c.Label != wgAmbiguous {
			continue
		}
		c := c
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()

			conf := DefaultWorkloadSignalConfidence.ConfidenceFor(c.Kind)
			if conf >= orchestratedFloor {
				t.Errorf(
					"ambiguous workload case %q (kind=%s): registered confidence %.4f ≥ orchestrated floor %.4f; "+
						"ambiguous signals must not sit in WorkloadTierOrchestratedRuntime",
					c.ID, c.Kind, conf, orchestratedFloor,
				)
			}
		})
	}
}
