package semanticeval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// TruthClass is the eval-facing authority class reported for one result.
type TruthClass string

const (
	// TruthClassExact means the result claims authoritative Eshu graph truth.
	TruthClassExact TruthClass = "exact"
	// TruthClassDerived means the result is deterministically computed from indexed state.
	TruthClassDerived TruthClass = "derived"
	// TruthClassSemanticCandidate means the result is relevance-ranked candidate context.
	TruthClassSemanticCandidate TruthClass = "semantic_candidate"
	// TruthClassStaleEvidence means the result is relevant but freshness-decayed or stale.
	TruthClassStaleEvidence TruthClass = "stale_evidence"
	// TruthClassUnsupported means the runtime cannot answer this case.
	TruthClassUnsupported TruthClass = "unsupported"
)

// Suite is a collection of semantic retrieval eval cases.
type Suite struct {
	Cases []Case `json:"cases"`
}

// Case describes one operator or developer question and its expected handles.
type Case struct {
	ID             string            `json:"id"`
	Question       string            `json:"question"`
	Scope          map[string]string `json:"scope,omitempty"`
	Expected       []ExpectedHandle  `json:"expected"`
	MustNotInclude []string          `json:"must_not_include,omitempty"`
}

// ExpectedHandle describes one relevant handle for an eval case.
type ExpectedHandle struct {
	Handle    string     `json:"handle"`
	Relevance int        `json:"relevance"`
	Required  bool       `json:"required,omitempty"`
	MaxTruth  TruthClass `json:"max_truth"`
}

// Run contains observed candidates for one evaluation run.
type Run struct {
	Results []CaseResult `json:"results"`
}

// CaseResult contains ranked candidates returned for one case.
type CaseResult struct {
	CaseID     string      `json:"case_id"`
	Candidates []Candidate `json:"candidates"`
	LatencyMS  float64     `json:"latency_ms,omitempty"`
}

// Candidate is one ranked result returned by a retrieval path.
type Candidate struct {
	Handle string     `json:"handle"`
	Truth  TruthClass `json:"truth"`
	Score  float64    `json:"score,omitempty"`
}

// LoadSuiteJSON decodes a strict JSON eval suite.
func LoadSuiteJSON(reader io.Reader) (Suite, error) {
	var suite Suite
	if err := decodeStrictJSON(reader, &suite); err != nil {
		return Suite{}, err
	}
	return suite, suite.Validate()
}

// LoadRunJSON decodes a strict JSON eval run.
func LoadRunJSON(reader io.Reader) (Run, error) {
	var run Run
	if err := decodeStrictJSON(reader, &run); err != nil {
		return Run{}, err
	}
	return run, run.Validate()
}

// Validate checks that the eval suite is explicit enough for scoring.
func (suite Suite) Validate() error {
	if len(suite.Cases) == 0 {
		return fmt.Errorf("suite must include at least one case")
	}
	seenCases := map[string]struct{}{}
	for _, evalCase := range suite.Cases {
		if err := evalCase.Validate(); err != nil {
			return err
		}
		if _, ok := seenCases[evalCase.ID]; ok {
			return fmt.Errorf("duplicate case id %q", evalCase.ID)
		}
		seenCases[evalCase.ID] = struct{}{}
	}
	return nil
}

// Validate checks that run results can be scored without ambiguity.
func (run Run) Validate() error {
	_, err := indexedRunResults(run)
	return err
}

// Validate checks one eval case for scoring invariants.
func (evalCase Case) Validate() error {
	if strings.TrimSpace(evalCase.ID) == "" {
		return fmt.Errorf("case id must not be blank")
	}
	if strings.TrimSpace(evalCase.Question) == "" {
		return fmt.Errorf("case %q question must not be blank", evalCase.ID)
	}
	if len(evalCase.Expected) == 0 {
		return fmt.Errorf("case %q must include expected handles", evalCase.ID)
	}
	seenHandles := map[string]struct{}{}
	hasRequired := false
	for _, expected := range evalCase.Expected {
		if err := expected.Validate(); err != nil {
			return fmt.Errorf("case %q: %w", evalCase.ID, err)
		}
		if _, ok := seenHandles[expected.Handle]; ok {
			return fmt.Errorf("case %q duplicate expected handle %q", evalCase.ID, expected.Handle)
		}
		seenHandles[expected.Handle] = struct{}{}
		if expected.Required {
			hasRequired = true
		}
	}
	if !hasRequired {
		return fmt.Errorf("case %q must include at least one required expected handle", evalCase.ID)
	}
	for _, handle := range evalCase.MustNotInclude {
		if strings.TrimSpace(handle) == "" {
			return fmt.Errorf("case %q must_not_include handle must not be blank", evalCase.ID)
		}
	}
	return nil
}

// Validate checks one expected handle.
func (expected ExpectedHandle) Validate() error {
	if strings.TrimSpace(expected.Handle) == "" {
		return fmt.Errorf("expected handle must not be blank")
	}
	if expected.Relevance <= 0 {
		return fmt.Errorf("expected handle %q relevance must be positive", expected.Handle)
	}
	return expected.MaxTruth.Validate()
}

// Validate checks that the truth class is known.
func (truth TruthClass) Validate() error {
	switch truth {
	case TruthClassExact, TruthClassDerived, TruthClassSemanticCandidate, TruthClassStaleEvidence, TruthClassUnsupported:
		return nil
	default:
		return fmt.Errorf("unknown truth class %q", truth)
	}
}

func decodeStrictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("json document contains trailing values")
	}
	return nil
}
