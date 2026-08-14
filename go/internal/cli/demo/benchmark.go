// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Time-to-first-answer (TTFA) is measured from command invocation to the first
// successful graph-authoritative answer envelope. `eshu demo up` already spans
// exactly that interval and reports it as TotalMillis, so the scorer reads the
// command's own measurement rather than timing it from outside and drifting.
const (
	// ModeCold is a run whose images were missing and had to be built or
	// pulled. The demo builds most of its images, so this is usually build cost
	// rather than download cost. This is what a first-time installer pays.
	ModeCold = "cold"
	// ModeWarm is a run with images already present.
	ModeWarm = "warm"
)

// ImageState is what the harness observed about the image cache before the
// run started. It exists so a declared mode can be contradicted by evidence.
type ImageState string

const (
	// ImagesUnknown means the harness did not probe. The mode is then taken
	// on trust and the cross-check records that it could not be performed.
	ImagesUnknown ImageState = ""
	// ImagesPresent means the demo images were in the local cache.
	ImagesPresent ImageState = "present"
	// ImagesAbsent means at least one demo image was missing and had to be
	// built or pulled.
	ImagesAbsent ImageState = "absent"
)

// RequiredPhases are the phases `eshu demo up` must account for.
//
// build is separate from up on purpose. A single bucket spanning
// `docker compose up -d --wait` covers image build, container start, corpus
// bootstrap, and the reducer drain all at once, so a regression in any of them
// looks identical. That bucket cannot answer "what got slower", which is the
// entire reason the per-phase numbers exist.
var RequiredPhases = []string{"preflight", "build", "up", "ready", "first_answer"}

// BenchmarkMeasurements carries the inputs the envelope cannot supply:
// which mode the operator ran, the target that mode is held to, and what the
// harness observed about the image cache.
type BenchmarkMeasurements struct {
	// Mode is ModeCold or ModeWarm.
	Mode string
	// Target is the TTFA budget for this mode. Zero means no target is set, in
	// which case the measured value is recorded without a pass/fail judgement.
	Target time.Duration
	// ImagesObserved is the probed image-cache state, or ImagesUnknown.
	ImagesObserved ImageState
}

// BenchmarkVerdict is the scorecard for one demo run.
type BenchmarkVerdict struct {
	// Mode echoes the cold/warm mode that was scored.
	Mode string `json:"mode"`
	// Pass is true only when every required criterion passed.
	Pass bool `json:"pass"`
	// TTFAMillis is the measured time to first answer.
	TTFAMillis int64 `json:"ttfa_millis"`
	// TargetMillis is the budget this run was held to; 0 when none was set.
	TargetMillis int64 `json:"target_millis"`
	// PhaseMillis echoes the per-phase breakdown behind TTFAMillis.
	PhaseMillis map[string]int64 `json:"phase_millis,omitempty"`
	// Criteria holds every scored row in stable order.
	Criteria []Criterion `json:"criteria"`
}

// Criterion returns the scored row for a name, or a zero-value criterion with
// that name when it was not scored, so a missing row is visibly distinct from
// a real outcome.
func (v BenchmarkVerdict) Criterion(name CriterionName) Criterion {
	for _, c := range v.Criteria {
		if c.Name == name {
			return c
		}
	}
	return Criterion{Name: name}
}

// FailureReasons lists the details of every required criterion that failed.
func (v BenchmarkVerdict) FailureReasons() []string {
	var reasons []string
	for _, c := range v.Criteria {
		if c.Required && c.Status == CriterionFail {
			reasons = append(reasons, fmt.Sprintf("%s: %s", c.Name, c.Detail))
		}
	}
	return reasons
}

// EvaluateBenchmark scores one `eshu demo --json` envelope for one mode.
//
// It is a pure function so every rejection is unit-testable without Docker.
// Cold and warm are scored separately by construction: a verdict carries one
// mode and one target, and there is no path that averages them. A blended
// number would understate what a first-time installer waits through, which is
// the number the demo exists to be honest about.
func EvaluateBenchmark(env Envelope, m BenchmarkMeasurements) BenchmarkVerdict {
	v := BenchmarkVerdict{
		Mode:         m.Mode,
		TTFAMillis:   env.Data.TotalMillis,
		TargetMillis: m.Target.Milliseconds(),
		PhaseMillis:  env.Data.PhaseMillis,
	}

	v.Criteria = append(v.Criteria,
		evaluateFirstAnswerCriterion(env),
		evaluateTruthCriterion(env),
		evaluateReadyCriterion(env),
		evaluatePhaseTimingsCriterion(env),
		evaluateTTFACriterion(env, m),
		evaluateModeCriterion(m),
	)

	v.Pass = true
	for _, c := range v.Criteria {
		if c.Required && c.Status != CriterionPass {
			v.Pass = false
			break
		}
	}
	return v
}

// evaluateFirstAnswerCriterion carries the first-run benchmark's
// health-only-rejection rule into the demo lane: a stack that came up is not
// an answer.
func evaluateFirstAnswerCriterion(env Envelope) Criterion {
	c := Criterion{Name: CriterionFirstAnswer, Required: true}
	switch {
	case env.Error != nil:
		c.Status = CriterionFail
		c.Detail = "run failed: " + env.Error.Message
	case strings.TrimSpace(env.Data.FirstAnswer.Answer) == "":
		c.Status = CriterionFail
		c.Detail = "no answer text; readiness alone is not a first answer"
	default:
		c.Status = CriterionPass
		c.Detail = "answered: " + firstLine(env.Data.FirstAnswer.Answer)
	}
	return c
}

// evaluateTruthCriterion rejects an answer with no provenance. An answer
// nobody can trace is not evidence, so it cannot count toward a TTFA claim.
func evaluateTruthCriterion(env Envelope) Criterion {
	c := Criterion{Name: CriterionTruthMetadata, Required: true}
	truth := env.Truth
	if len(truth) == 0 {
		truth = env.Data.FirstAnswer.Truth
	}
	if len(truth) == 0 {
		c.Status = CriterionFail
		c.Detail = "answer carries no truth labels"
		return c
	}
	c.Status = CriterionPass
	c.Detail = "truth labels: " + strings.Join(sortedKeys(truth), ", ")
	return c
}

// evaluateReadyCriterion asserts indexing completeness, not health.
func evaluateReadyCriterion(env Envelope) Criterion {
	c := Criterion{Name: CriterionRepoIndexed, Required: true}
	if !env.Data.Ready {
		c.Status = CriterionFail
		c.Detail = "stack did not reach indexing completeness"
		return c
	}
	c.Status = CriterionPass
	c.Detail = "indexing complete"
	return c
}

// evaluatePhaseTimingsCriterion requires the full breakdown behind the
// total. This is required rather than informational: a total whose composition
// is unknown cannot be attributed, so it cannot be published as a measurement.
func evaluatePhaseTimingsCriterion(env Envelope) Criterion {
	c := Criterion{Name: CriterionPhaseTimings, Required: true}
	var missing []string
	for _, phase := range RequiredPhases {
		if _, ok := env.Data.PhaseMillis[phase]; !ok {
			missing = append(missing, phase)
		}
	}
	if len(missing) > 0 {
		c.Status = CriterionFail
		c.Detail = "missing phase timing(s): " + strings.Join(missing, ", ")
		return c
	}
	if env.Data.TotalMillis <= 0 {
		c.Status = CriterionFail
		c.Detail = "total_millis is not positive; there is no measured total to score"
		return c
	}
	c.Status = CriterionPass
	c.Detail = "all phases recorded: " + strings.Join(RequiredPhases, ", ")
	return c
}

// evaluateTTFACriterion scores the measured total against the mode's
// target. With no target set the row records the value without judging it,
// which is how a first measurement run establishes the target in the first
// place rather than being failed by one that does not exist yet.
func evaluateTTFACriterion(env Envelope, m BenchmarkMeasurements) Criterion {
	c := Criterion{Name: CriterionTimeToAnswer, Required: true}
	measured := time.Duration(env.Data.TotalMillis) * time.Millisecond
	if env.Data.TotalMillis <= 0 {
		c.Status = CriterionFail
		c.Detail = "no measured time to first answer"
		return c
	}
	if m.Target <= 0 {
		c.Status = CriterionNotMeasured
		c.Required = false
		c.Detail = fmt.Sprintf("%s measured, no %s target set", measured.Round(time.Millisecond), m.Mode)
		return c
	}
	if measured > m.Target {
		c.Status = CriterionFail
		c.Detail = fmt.Sprintf("%s TTFA %s exceeds target %s", m.Mode, measured.Round(time.Millisecond), m.Target)
		return c
	}
	c.Status = CriterionPass
	c.Detail = fmt.Sprintf("%s TTFA %s within target %s", m.Mode, measured.Round(time.Millisecond), m.Target)
	return c
}

// evaluateModeCriterion cross-checks the declared mode against the probed
// image cache.
//
// A declared label is the weakest part of the measurement: a warm run
// published as COLD understates the wait a first-time installer actually has,
// and nothing downstream can detect it from the number alone. When the harness
// probed, a contradiction fails the run. When it did not probe, the row says so
// instead of implying a check that never happened.
func evaluateModeCriterion(m BenchmarkMeasurements) Criterion {
	c := Criterion{Name: CriterionModeObserved, Required: true}
	switch m.Mode {
	case ModeCold, ModeWarm:
	default:
		c.Status = CriterionFail
		c.Detail = fmt.Sprintf("mode %q is neither %q nor %q", m.Mode, ModeCold, ModeWarm)
		return c
	}
	if m.ImagesObserved == ImagesUnknown {
		c.Status = CriterionNotMeasured
		c.Required = false
		c.Detail = "image cache not probed; declared mode " + m.Mode + " taken on trust"
		return c
	}
	want := ImagesPresent
	if m.Mode == ModeCold {
		want = ImagesAbsent
	}
	if m.ImagesObserved != want {
		c.Status = CriterionFail
		c.Detail = fmt.Sprintf("declared %s but images were %s; a mislabelled run publishes the wrong number",
			m.Mode, m.ImagesObserved)
		return c
	}
	c.Status = CriterionPass
	c.Detail = fmt.Sprintf("declared %s and images were %s", m.Mode, m.ImagesObserved)
	return c
}

// firstLine trims a rendered answer to its first line for scorecard output.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// sortedKeys renders map keys deterministically so two scorings of the
// same envelope produce byte-identical output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
