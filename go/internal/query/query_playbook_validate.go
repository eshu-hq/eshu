// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// rawCypherTools lists the tool names that expose raw graph query languages.
// Playbooks describe normal first-class workflows and must never include these
// as a step; Validate rejects any playbook that does.
func rawCypherTools() []string {
	return []string{
		"execute_cypher_query",
		"execute_language_query",
		"visualize_graph_query",
	}
}

func isRawCypherTool(tool string) bool {
	for _, raw := range rawCypherTools() {
		if tool == raw {
			return true
		}
	}
	return false
}

// Validate checks the structural contract of a playbook: identity fields, at
// least one bounded step, valid input references, declared expectations, and no
// raw Cypher step. It performs no I/O and is safe to call at package init or in
// tests. It returns the first violation found.
func (pb QueryPlaybook) Validate() error {
	if strings.TrimSpace(pb.ID) == "" {
		return fmt.Errorf("playbook ID is required")
	}
	if strings.TrimSpace(pb.Name) == "" {
		return fmt.Errorf("playbook %q: name is required", pb.ID)
	}
	if strings.TrimSpace(pb.Version) == "" {
		return fmt.Errorf("playbook %q: version is required", pb.ID)
	}
	if strings.TrimSpace(pb.PromptFamily) == "" {
		return fmt.Errorf("playbook %q: prompt_family is required", pb.ID)
	}
	if len(pb.Steps) == 0 {
		return fmt.Errorf("playbook %q: at least one step is required", pb.ID)
	}
	if len(pb.FailureModes) == 0 {
		return fmt.Errorf("playbook %q: at least one failure mode must be declared", pb.ID)
	}

	declared, err := pb.validateInputs()
	if err != nil {
		return err
	}

	seenStep := make(map[string]struct{}, len(pb.Steps))
	for i, step := range pb.Steps {
		if err := step.validate(pb.ID, i, declared); err != nil {
			return err
		}
		if _, dup := seenStep[step.ID]; dup {
			return fmt.Errorf("playbook %q: duplicate step ID %q", pb.ID, step.ID)
		}
		seenStep[step.ID] = struct{}{}
	}

	for i, fm := range pb.FailureModes {
		if strings.TrimSpace(fm.Condition) == "" || strings.TrimSpace(fm.Meaning) == "" ||
			strings.TrimSpace(fm.Fallback) == "" {
			return fmt.Errorf("playbook %q: failure mode %d must declare condition, meaning, and fallback", pb.ID, i)
		}
	}
	return nil
}

// validateInputs checks the declared inputs and returns the declared-name set
// used to validate step parameter references.
func (pb QueryPlaybook) validateInputs() (map[string]struct{}, error) {
	declared := make(map[string]struct{}, len(pb.RequiredInputs))
	for _, in := range pb.RequiredInputs {
		if strings.TrimSpace(in.Name) == "" {
			return nil, fmt.Errorf("playbook %q: input name is required", pb.ID)
		}
		if in.Type != PlaybookInputString && in.Type != PlaybookInputIdentifier {
			return nil, fmt.Errorf("playbook %q: input %q has unknown type %q", pb.ID, in.Name, in.Type)
		}
		if _, dup := declared[in.Name]; dup {
			return nil, fmt.Errorf("playbook %q: duplicate input %q", pb.ID, in.Name)
		}
		declared[in.Name] = struct{}{}
	}
	return declared, nil
}

func (s PlaybookStep) validate(playbookID string, index int, declaredInputs map[string]struct{}) error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("playbook %q: step %d is missing an ID", playbookID, index)
	}
	if strings.TrimSpace(s.Tool) == "" {
		return fmt.Errorf("playbook %q: step %q is missing a tool", playbookID, s.ID)
	}
	if isRawCypherTool(s.Tool) {
		return fmt.Errorf("playbook %q: step %q references raw query tool %q, which is not permitted in a playbook", playbookID, s.ID, s.Tool)
	}
	if strings.TrimSpace(s.EvidenceExpected) == "" {
		return fmt.Errorf("playbook %q: step %q must declare expected evidence", playbookID, s.ID)
	}
	if !knownTruthClass(s.ExpectedTruth) {
		return fmt.Errorf("playbook %q: step %q has unknown expected truth class %q", playbookID, s.ID, s.ExpectedTruth)
	}
	if err := s.validateParams(playbookID, declaredInputs); err != nil {
		return err
	}
	for _, d := range s.Drilldowns {
		if strings.TrimSpace(d.Tool) == "" {
			return fmt.Errorf("playbook %q: step %q has a drilldown with no tool", playbookID, s.ID)
		}
		if isRawCypherTool(d.Tool) {
			return fmt.Errorf("playbook %q: step %q drilldown references raw query tool %q", playbookID, s.ID, d.Tool)
		}
	}
	return nil
}

func (s PlaybookStep) validateParams(playbookID string, declaredInputs map[string]struct{}) error {
	seenParam := make(map[string]struct{}, len(s.Params))
	for _, param := range s.Params {
		if strings.TrimSpace(param.Name) == "" {
			return fmt.Errorf("playbook %q: step %q has a param with no name", playbookID, s.ID)
		}
		if _, dup := seenParam[param.Name]; dup {
			return fmt.Errorf("playbook %q: step %q has duplicate param %q", playbookID, s.ID, param.Name)
		}
		seenParam[param.Name] = struct{}{}
		if err := param.validateSingleSource(playbookID, s.ID); err != nil {
			return err
		}
		if param.FromInput != "" {
			if _, ok := declaredInputs[param.FromInput]; !ok {
				return fmt.Errorf("playbook %q: step %q param %q references undeclared input %q", playbookID, s.ID, param.Name, param.FromInput)
			}
		}
	}
	return nil
}

// validateSingleSource rejects a param that declares more than one value
// source. A param must bind from exactly one of an input, a constant int, a
// constant bool, or a constant string; declaring several is an authoring error
// that source() would resolve silently by precedence.
func (p PlaybookParam) validateSingleSource(playbookID, stepID string) error {
	sources := 0
	if p.FromInput != "" {
		sources++
	}
	if p.hasConstInt {
		sources++
	}
	if p.hasConstBool {
		sources++
	}
	if p.ConstString != "" {
		sources++
	}
	if sources > 1 {
		return fmt.Errorf("playbook %q: step %q param %q declares multiple value sources", playbookID, stepID, p.Name)
	}
	return nil
}

func knownTruthClass(c AnswerTruthClass) bool {
	switch c {
	case AnswerTruthDeterministic, AnswerTruthDerived, AnswerTruthFallback,
		AnswerTruthSemanticObservation, AnswerTruthCodeHint, AnswerTruthUnsupported:
		return true
	default:
		return false
	}
}
