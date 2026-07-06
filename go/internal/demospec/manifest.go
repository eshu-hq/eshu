// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package demospec loads and validates the demo-first-answers manifest
// (specs/demo-first-answers.v1.yaml), the acceptance oracle for issue #4741.
package demospec

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFileName is the demo-first-answers spec's file name inside the
// repository's specs directory.
const ManifestFileName = "demo-first-answers.v1.yaml"

// SurfaceKind enumerates the read-surface kinds a demo question can bind to.
// Every kind must resolve to an existing, already-shipped read surface; none
// of them introduce new query capability.
type SurfaceKind string

// Supported SurfaceKind values.
const (
	SurfaceKindPlaybook SurfaceKind = "playbook"
	SurfaceKindMCP      SurfaceKind = "mcp"
	SurfaceKindCLI      SurfaceKind = "cli"
	SurfaceKindHTTP     SurfaceKind = "http"
)

var validSurfaceKinds = map[SurfaceKind]struct{}{
	SurfaceKindPlaybook: {},
	SurfaceKindMCP:      {},
	SurfaceKindCLI:      {},
	SurfaceKindHTTP:     {},
}

// Manifest is the parsed demo-first-answers spec: exactly five questions,
// each pinned to a bounded call on an existing read surface plus the golden
// corpus artifacts (cassette families and fixture repos) that back it.
type Manifest struct {
	// Version is the spec schema version (e.g. "demo-first-answers/v1").
	Version string
	// UpdatedAt is the spec's last-reviewed date, "YYYY-MM-DD".
	UpdatedAt string
	// Issue is the GitHub issue number this spec is the acceptance oracle for.
	Issue int
	// ParentIssue is the epic issue number this spec's issue belongs to.
	ParentIssue int
	// Owners lists the team/domain owners responsible for keeping this spec
	// current.
	Owners []string
	// Purpose explains what this manifest is for.
	Purpose string
	// Design explains the demo family/question-set design decisions.
	Design string
	// Questions are the five demo questions this manifest declares. The
	// loader rejects a manifest whose length is not exactly five.
	Questions []Question
}

// Question is one demo-first-answers question: a human-readable prompt, the
// correlation it demonstrates, the bounded read surface that answers it, the
// expected answer shape, and the golden-corpus artifacts it depends on.
type Question struct {
	// ID is the question's stable identifier (e.g. "q1_code_to_deployment").
	ID string
	// QuestionText is the human-readable prompt.
	QuestionText string
	// CorrelationKind names the correlation category this question
	// demonstrates (e.g. "code_to_deployment").
	CorrelationKind string
	// Surface is the bounded read surface that answers this question.
	Surface Surface
	// Notes carries any design rationale specific to this question (for
	// example why one MCP tool was chosen over an alternative).
	Notes string
	// ExpectedAnswer describes the response shape and the correlations the
	// answer demonstrates.
	ExpectedAnswer ExpectedAnswer
	// Artifacts are the cassette families and fixture repos this question's
	// answer depends on.
	Artifacts Artifacts
}

// Surface is the bounded, already-existing read surface a question binds to.
type Surface struct {
	// Kind is one of playbook, mcp, cli, or http.
	Kind SurfaceKind
	// Ref is the surface identifier: a playbook ID, an MCP tool name, the
	// literal CLI query-shape key, or the literal HTTP query-shape key from
	// testdata/golden/e2e-20repo-snapshot.json.
	Ref string
	// Arguments are the call arguments used against the surface (illustrative
	// for docs; the referential-integrity test does not replay them).
	Arguments map[string]any
	// Execute is the concrete, gate-executable call that produces this
	// question's answer. It is required for any surface the gate cannot call
	// directly — a playbook (a playbook ID is not a callable endpoint) or a cli
	// verb (the gate has no CLI seam) — so the manifest names the underlying
	// mcp tool or http route the demo-answers gate invokes. It is optional for
	// mcp/http surfaces, whose own Kind/Ref/Arguments are already executable;
	// nil there means "run the surface itself".
	Execute *ExecuteTarget
}

// ExecuteTarget is a concrete, gate-executable read call: an MCP tool or an
// HTTP route plus its arguments. The demo-answers phase of the golden-corpus
// gate uses it to fetch a live answer for a question whose Surface is not
// directly callable (a playbook).
type ExecuteTarget struct {
	// Kind is the executable surface kind: mcp or http (not playbook or cli).
	Kind SurfaceKind
	// Ref is the MCP tool name, or the literal "METHOD /path" HTTP key.
	Ref string
	// Arguments are the call arguments (MCP tool arguments, or query/path
	// parameters for an HTTP route).
	Arguments map[string]any
}

// ExpectedAnswer describes what a correct answer looks like: the response
// fields or JSON paths a caller can assert on, and the required_correlations
// (rc-NN) IDs from the golden snapshot the answer demonstrates.
type ExpectedAnswer struct {
	// RequiredResponseFields are top-level (or dotted, for nested) field
	// names expected in the tool/playbook response payload.
	RequiredResponseFields []string
	// RequiredJSONPaths are dotted JSON paths expected in an HTTP response
	// body (used instead of RequiredResponseFields for HTTP surfaces whose
	// payload nests under an envelope key such as "data").
	RequiredJSONPaths []string
	// DemonstratesCorrelations lists the required_correlations (rc-NN) IDs
	// from testdata/golden/e2e-20repo-snapshot.json that this question's
	// answer proves ran.
	DemonstratesCorrelations []string
	// MinimumResults is the minimum number of items the answer's primary
	// result array must contain for the answer to count as populated. Zero
	// (the default) asserts only that the required fields/paths are present —
	// correct for object-shaped answers with no result array. A positive value
	// is asserted by the demo-answers gate phase against the first
	// array-valued RequiredResponseFields entry, so a demo answer that
	// silently regresses to empty turns the gate red.
	MinimumResults int
}

// Artifacts are the golden-corpus inputs a question's answer depends on.
type Artifacts struct {
	// Cassettes are cassette family directory names under testdata/cassettes/
	// (e.g. "tempo" for testdata/cassettes/tempo/supply-chain-demo.json).
	Cassettes []string
	// Repos are fixture directory names under tests/fixtures/ecosystems/.
	Repos []string
}

// manifestFile, questionFile, surfaceFile, expectedAnswerFile, and
// artifactsFile are the YAML wire shapes; LoadManifest converts them into the
// exported Manifest/Question/Surface/ExpectedAnswer/Artifacts types above so
// callers work with Go-idiomatic field names instead of yaml tags.
type manifestFile struct {
	Version     string         `yaml:"version"`
	UpdatedAt   string         `yaml:"updated_at"`
	Issue       int            `yaml:"issue"`
	ParentIssue int            `yaml:"parent_issue"`
	Owners      []string       `yaml:"owners"`
	Purpose     string         `yaml:"purpose"`
	Design      string         `yaml:"design"`
	Questions   []questionFile `yaml:"questions"`
}

type questionFile struct {
	ID              string             `yaml:"id"`
	Question        string             `yaml:"question"`
	CorrelationKind string             `yaml:"correlation_kind"`
	Surface         surfaceFile        `yaml:"surface"`
	Notes           string             `yaml:"notes"`
	ExpectedAnswer  expectedAnswerFile `yaml:"expected_answer"`
	Artifacts       artifactsFile      `yaml:"artifacts"`
}

type surfaceFile struct {
	Kind      string             `yaml:"kind"`
	Ref       string             `yaml:"ref"`
	Arguments map[string]any     `yaml:"arguments"`
	Execute   *executeTargetFile `yaml:"execute"`
}

type executeTargetFile struct {
	Kind      string         `yaml:"kind"`
	Ref       string         `yaml:"ref"`
	Arguments map[string]any `yaml:"arguments"`
}

type expectedAnswerFile struct {
	RequiredResponseFields  []string `yaml:"required_response_fields"`
	RequiredJSONPaths       []string `yaml:"required_json_paths"`
	DemonstratesCorrelation []string `yaml:"demonstrates_correlations"`
	MinimumResults          int      `yaml:"minimum_results"`
}

type artifactsFile struct {
	Cassettes []string `yaml:"cassettes"`
	Repos     []string `yaml:"repos"`
}

// requiredQuestionCount is the exact number of demo questions the manifest
// must declare (issue #4741 acceptance criterion: exactly five).
const requiredQuestionCount = 5

// LoadManifest reads and validates the demo-first-answers manifest at path.
// A missing file, a manifest that is not exactly five questions, a blank
// required field, an unknown surface kind, an answer shape with neither
// required_response_fields nor required_json_paths, or a question with no
// demonstrated correlation is an error: this manifest is the acceptance
// oracle for issue #4741, so a malformed manifest must fail loudly rather
// than silently under-specify the demo questions it is supposed to pin down.
func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-owned path under specs/, not external input
	if err != nil {
		return Manifest{}, fmt.Errorf("read demo-first-answers manifest %s: %w", path, err)
	}
	var parsed manifestFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return Manifest{}, fmt.Errorf("parse demo-first-answers manifest %s: %w", path, err)
	}

	if strings.TrimSpace(parsed.Version) == "" {
		return Manifest{}, fmt.Errorf("demo-first-answers manifest %s: version is blank", path)
	}
	if len(parsed.Questions) != requiredQuestionCount {
		return Manifest{}, fmt.Errorf(
			"demo-first-answers manifest %s: expected exactly %d questions, found %d",
			path, requiredQuestionCount, len(parsed.Questions),
		)
	}

	m := Manifest{
		Version:     parsed.Version,
		UpdatedAt:   parsed.UpdatedAt,
		Issue:       parsed.Issue,
		ParentIssue: parsed.ParentIssue,
		Owners:      parsed.Owners,
		Purpose:     parsed.Purpose,
		Design:      parsed.Design,
	}

	seenIDs := map[string]struct{}{}
	for _, qf := range parsed.Questions {
		q, err := convertQuestion(path, qf)
		if err != nil {
			return Manifest{}, err
		}
		if _, dup := seenIDs[q.ID]; dup {
			return Manifest{}, fmt.Errorf("demo-first-answers manifest %s: question id %q declared twice", path, q.ID)
		}
		seenIDs[q.ID] = struct{}{}
		m.Questions = append(m.Questions, q)
	}
	return m, nil
}

// convertQuestion validates and converts one YAML question entry.
func convertQuestion(path string, qf questionFile) (Question, error) {
	id := strings.TrimSpace(qf.ID)
	if id == "" {
		return Question{}, fmt.Errorf("demo-first-answers manifest %s: a question has a blank id", path)
	}
	if strings.TrimSpace(qf.Question) == "" {
		return Question{}, fmt.Errorf("demo-first-answers manifest %s: question %q has blank question text", path, id)
	}
	if strings.TrimSpace(qf.CorrelationKind) == "" {
		return Question{}, fmt.Errorf("demo-first-answers manifest %s: question %q has blank correlation_kind", path, id)
	}

	surface, err := convertSurface(path, id, qf.Surface)
	if err != nil {
		return Question{}, err
	}

	if len(qf.ExpectedAnswer.RequiredResponseFields) == 0 && len(qf.ExpectedAnswer.RequiredJSONPaths) == 0 {
		return Question{}, fmt.Errorf(
			"demo-first-answers manifest %s: question %q has no required_response_fields or required_json_paths",
			path, id,
		)
	}
	if len(qf.ExpectedAnswer.DemonstrateCorrelationsOrEmpty()) == 0 {
		return Question{}, fmt.Errorf("demo-first-answers manifest %s: question %q demonstrates no correlations", path, id)
	}
	if qf.ExpectedAnswer.MinimumResults < 0 {
		return Question{}, fmt.Errorf("demo-first-answers manifest %s: question %q has negative minimum_results %d", path, id, qf.ExpectedAnswer.MinimumResults)
	}

	return Question{
		ID:              id,
		QuestionText:    strings.TrimSpace(qf.Question),
		CorrelationKind: strings.TrimSpace(qf.CorrelationKind),
		Surface:         surface,
		Notes:           strings.TrimSpace(qf.Notes),
		ExpectedAnswer: ExpectedAnswer{
			RequiredResponseFields:   qf.ExpectedAnswer.RequiredResponseFields,
			RequiredJSONPaths:        qf.ExpectedAnswer.RequiredJSONPaths,
			DemonstratesCorrelations: qf.ExpectedAnswer.DemonstrateCorrelationsOrEmpty(),
			MinimumResults:           qf.ExpectedAnswer.MinimumResults,
		},
		Artifacts: Artifacts{
			Cassettes: qf.Artifacts.Cassettes,
			Repos:     qf.Artifacts.Repos,
		},
	}, nil
}

// validExecuteKinds are the surface kinds an execute target may use: only the
// two directly-callable read surfaces, never a playbook (not callable) or cli
// (not reachable over the gate's API/MCP seams).
var validExecuteKinds = map[SurfaceKind]struct{}{
	SurfaceKindMCP:  {},
	SurfaceKindHTTP: {},
}

// convertSurface validates and converts a question's surface block, including
// the optional execute target that makes a playbook surface gate-executable.
func convertSurface(path, id string, sf surfaceFile) (Surface, error) {
	kind := SurfaceKind(strings.TrimSpace(sf.Kind))
	if _, ok := validSurfaceKinds[kind]; !ok {
		return Surface{}, fmt.Errorf("demo-first-answers manifest %s: question %q has invalid surface kind %q", path, id, sf.Kind)
	}
	ref := strings.TrimSpace(sf.Ref)
	if ref == "" {
		return Surface{}, fmt.Errorf("demo-first-answers manifest %s: question %q has blank surface ref", path, id)
	}
	exec, err := convertExecuteTarget(path, id, kind, sf.Execute)
	if err != nil {
		return Surface{}, err
	}
	return Surface{Kind: kind, Ref: ref, Arguments: sf.Arguments, Execute: exec}, nil
}

// convertExecuteTarget validates the optional execute block. A playbook surface
// requires one (its ref is not directly callable, so the manifest must name the
// underlying mcp tool or http route the gate invokes); an mcp/http/cli surface
// may omit it. When present it must name an mcp tool or an http route.
func convertExecuteTarget(path, id string, surfaceKind SurfaceKind, ef *executeTargetFile) (*ExecuteTarget, error) {
	if ef == nil {
		// A surface the gate cannot call directly (playbook: a playbook id is not
		// a callable endpoint; cli: the gate has no CLI seam, only API/MCP) must
		// name the underlying mcp tool or http route the demo-answers gate
		// invokes. The directly-callable kinds (mcp, http) may omit execute.
		if _, callable := validExecuteKinds[surfaceKind]; !callable {
			return nil, fmt.Errorf(
				"demo-first-answers manifest %s: question %q has a %s surface the gate cannot call directly but no surface.execute; name the underlying mcp tool or http route the demo-answers gate invokes",
				path, id, surfaceKind,
			)
		}
		return nil, nil
	}
	kind := SurfaceKind(strings.TrimSpace(ef.Kind))
	if _, ok := validExecuteKinds[kind]; !ok {
		return nil, fmt.Errorf("demo-first-answers manifest %s: question %q surface.execute has invalid kind %q (want mcp or http)", path, id, ef.Kind)
	}
	ref := strings.TrimSpace(ef.Ref)
	if ref == "" {
		return nil, fmt.Errorf("demo-first-answers manifest %s: question %q surface.execute has blank ref", path, id)
	}
	return &ExecuteTarget{Kind: kind, Ref: ref, Arguments: ef.Arguments}, nil
}

// DemonstrateCorrelationsOrEmpty returns the demonstrated correlation IDs, or
// an empty (nil) slice if none were declared. It exists so convertQuestion's
// blank-check and the final assignment share one accessor instead of
// repeating the yaml field name.
func (e expectedAnswerFile) DemonstrateCorrelationsOrEmpty() []string {
	return e.DemonstratesCorrelation
}
