// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// InputType enumerates the value kinds a playbook input accepts. The set
// is intentionally small: playbooks describe bounded, machine-readable workflows
// over first-class tools, not arbitrary programs.
type InputType string

const (
	// InputString is a free-form string input such as a service name or
	// code topic.
	InputString InputType = "string"
	// InputIdentifier is a canonical identifier input such as a repo ID,
	// workload ID, or finding ID.
	InputIdentifier InputType = "identifier"
)

// Input declares one input a playbook requires or accepts. Inputs are
// the only external state a playbook may read; a resolver never invents a value
// that is not supplied or defaulted here.
type Input struct {
	// Name is the input key callers pass to Resolve.
	Name string `json:"name"`
	// Type is the declared value kind.
	Type InputType `json:"type"`
	// Required marks an input that Resolve must reject when absent.
	Required bool `json:"required"`
	// Description documents the input for prompt-surface authors.
	Description string `json:"description,omitempty"`
}

// ParamSource enumerates where a resolved parameter value comes from. A
// parameter is either bound from a declared input or set to a constant declared
// in the playbook. There is no third, hidden source.
type ParamSource string

const (
	// ParamFromInput binds the parameter to a declared input value.
	ParamFromInput ParamSource = "input"
	// ParamConstString binds the parameter to a constant string.
	ParamConstString ParamSource = "const_string"
	// ParamConstInt binds the parameter to a constant integer, used for
	// default limits and other bounds.
	ParamConstInt ParamSource = "const_int"
	// ParamConstBool binds the parameter to a constant boolean, used for
	// flag arguments such as opting a step into reranking.
	ParamConstBool ParamSource = "const_bool"
)

// Param declares one bounded argument for a step. Exactly one source is
// active, determined by FromInput / ConstInt / ConstBool / ConstString. The
// resolver produces a concrete value from this declaration alone.
type Param struct {
	// Name is the argument key passed to the tool.
	Name string `json:"name"`
	// FromInput, when non-empty, binds Name to the named declared input.
	FromInput string `json:"from_input,omitempty"`
	// ConstString sets Name to a constant string when FromInput is empty.
	ConstString string `json:"const_string,omitempty"`
	// ConstInt sets Name to a constant integer. Used for default limits so every
	// resolved call is bounded.
	ConstInt int `json:"const_int,omitempty"`
	// hasConstInt disambiguates a deliberate zero constant integer from an unset
	// field. It is package-internal because the catalog uses the helper
	// constructors below.
	hasConstInt bool
	// ConstBool sets Name to a constant boolean flag argument.
	ConstBool bool `json:"const_bool,omitempty"`
	// hasConstBool disambiguates a deliberate false constant boolean from an unset
	// field. It is package-internal for the same reason as hasConstInt.
	hasConstBool bool
}

func (p Param) source() ParamSource {
	switch {
	case p.FromInput != "":
		return ParamFromInput
	case p.hasConstInt:
		return ParamConstInt
	case p.hasConstBool:
		return ParamConstBool
	default:
		return ParamConstString
	}
}

// InputParam binds a tool argument to a declared playbook input.
func InputParam(name, fromInput string) Param {
	return Param{Name: name, FromInput: fromInput}
}

// LimitParam declares a default bounded limit for a tool argument. Using this
// helper keeps every list step explicitly bounded in the catalog.
func LimitParam(name string, value int) Param {
	return Param{Name: name, ConstInt: value, hasConstInt: true}
}

// ConstStringParam declares a constant string argument.
func ConstStringParam(name, value string) Param {
	return Param{Name: name, ConstString: value}
}

// BoolParam declares a constant boolean flag argument so a resolved call carries
// a real bool (not a "true" string) for flags such as rerank.
func BoolParam(name string, value bool) Param {
	return Param{Name: name, ConstBool: value, hasConstBool: true}
}

// Drilldown declares an optional follow-up call surfaced after a step
// when the operator needs more evidence. It carries the same recommended-next-
// call intent the evidence-citation surface already uses.
type Drilldown struct {
	// Tool is the first-class tool the drilldown calls.
	Tool string `json:"tool"`
	// Reason explains when to take the drilldown.
	Reason string `json:"reason"`
}

// Step is one ordered, bounded call in a playbook. It names a first-class
// tool (never raw Cypher), declares bounded parameters, and states the expected
// truth class and evidence so a resolver yields a fully specified call without
// agent-side assumptions.
type Step struct {
	// ID is the stable step identifier within the playbook.
	ID string `json:"id"`
	// Tool is the first-class MCP tool or route name the step invokes.
	Tool string `json:"tool"`
	// Params declares the bounded arguments for the call.
	Params []Param `json:"params,omitempty"`
	// ExpectedTruth is the truth class the step is expected to yield, reusing the
	// AnswerPacket truth taxonomy rather than defining a new one.
	ExpectedTruth querycontract.AnswerTruthClass `json:"expected_truth"`
	// EvidenceExpected describes the evidence the step should produce, for example
	// "service dossier with deployment lanes and evidence handles".
	EvidenceExpected string `json:"evidence_expected"`
	// Drilldowns are optional follow-up calls for deeper evidence.
	Drilldowns []Drilldown `json:"drilldowns,omitempty"`
}

// FailureMode declares a truth or error condition a step family can hit
// and the recommended fallback. Declaring failure modes is mandatory so callers
// never improvise an undefined recovery path.
type FailureMode struct {
	// Condition is the truth/error condition, for example "service not found" or
	// "result truncated".
	Condition string `json:"condition"`
	// Meaning explains what the condition implies for the answer.
	Meaning string `json:"meaning"`
	// Fallback is the recommended recovery, expressed as a first-class tool or a
	// bounded action.
	Fallback string `json:"fallback"`
}

// Definition is a deterministic, bounded, versioned description of a common
// starter-prompt or cookbook workflow. It is data, not executable code: it names
// the ordered first-class tool calls, their bounded parameters, the expected
// truth and evidence per step, and the declared failure modes. A playbook
// describes how to reach an AnswerPacket for a prompt family; it reuses the
// AnswerPacket truth taxonomy and the evidence-citation recommended-next-call
// shape rather than redefining them.
type Definition struct {
	// ID is the stable catalog identifier.
	ID string `json:"id"`
	// Name is the human-readable playbook name.
	Name string `json:"name"`
	// Version is the semantic version of this playbook definition. Catalog
	// stability is asserted against ID+Version.
	Version string `json:"version"`
	// PromptFamily is the canonical prompt family the playbook serves, aligned
	// with AnswerPacket.PromptFamily.
	PromptFamily string `json:"prompt_family"`
	// Description documents the workflow for prompt-surface authors.
	Description string `json:"description,omitempty"`
	// RequiredInputs declares the inputs the playbook reads. Resolution uses only
	// these inputs and the constants declared on steps.
	RequiredInputs []Input `json:"required_inputs"`
	// Steps is the ordered list of bounded calls.
	Steps []Step `json:"steps"`
	// FailureModes declares the truth/error conditions and recommended fallbacks.
	FailureModes []FailureMode `json:"failure_modes"`
}

// VersionRef is a compact (ID, Version) pair used for catalog stability
// assertions and machine-readable catalog listing.
type VersionRef struct {
	// ID is the playbook identifier.
	ID string `json:"id"`
	// Version is the playbook version.
	Version string `json:"version"`
}

// Summary is the compact, default-view projection of a Definition: enough to
// identify and describe a playbook without its ordered steps, required
// inputs, or failure modes, which dominate a Definition's serialized size
// (#6795). Full detail remains reachable via view=full or by resolving the
// playbook with resolve_query_playbook.
type Summary struct {
	// ID is the stable catalog identifier.
	ID string `json:"id"`
	// Name is the human-readable playbook name.
	Name string `json:"name"`
	// Version is the semantic version of this playbook definition.
	Version string `json:"version"`
	// PromptFamily is the canonical prompt family the playbook serves.
	PromptFamily string `json:"prompt_family"`
	// Description documents the workflow in one line for prompt-surface authors.
	Description string `json:"description,omitempty"`
}

// ToSummary projects pb into its compact wire shape.
func (pb Definition) ToSummary() Summary {
	return Summary{
		ID:           pb.ID,
		Name:         pb.Name,
		Version:      pb.Version,
		PromptFamily: pb.PromptFamily,
		Description:  pb.Description,
	}
}

// ResolvedCall is one fully specified, bounded call produced by resolving a
// playbook step against concrete inputs. Arguments hold concrete values only;
// there is no remaining template to interpolate.
type ResolvedCall struct {
	// StepID is the originating step identifier.
	StepID string `json:"step_id"`
	// Tool is the first-class tool to call.
	Tool string `json:"tool"`
	// Arguments are the concrete, bounded call arguments.
	Arguments map[string]any `json:"arguments"`
	// ExpectedTruth is the truth class the call is expected to yield.
	ExpectedTruth querycontract.AnswerTruthClass `json:"expected_truth"`
	// EvidenceExpected describes the expected evidence.
	EvidenceExpected string `json:"evidence_expected"`
	// Drilldowns are the optional follow-up calls declared for the step.
	Drilldowns []Drilldown `json:"drilldowns,omitempty"`
}

// ResolvedPlaybook is the deterministic output of resolving a playbook against
// concrete inputs: the ordered bounded calls plus the declared failure modes a
// caller must handle. It depends on no external or live backend state.
type ResolvedPlaybook struct {
	// PlaybookID is the resolved playbook identifier.
	PlaybookID string `json:"playbook_id"`
	// Version is the resolved playbook version.
	Version string `json:"version"`
	// PromptFamily is the prompt family the playbook serves.
	PromptFamily string `json:"prompt_family"`
	// Calls is the ordered, fully specified call sequence.
	Calls []ResolvedCall `json:"calls"`
	// FailureModes carries the declared failure handling forward to the caller.
	FailureModes []FailureMode `json:"failure_modes"`
}

// Resolve produces the deterministic ordered call sequence for the playbook from
// the supplied inputs. It validates the playbook, rejects undeclared inputs,
// requires every Required input, binds each step's params from inputs or
// declared constants, and returns a fully specified ResolvedPlaybook. It reads
// no external state and is referentially transparent: equal inputs always yield
// an equal result.
func (pb Definition) Resolve(inputs map[string]string) (ResolvedPlaybook, error) {
	if err := pb.Validate(); err != nil {
		return ResolvedPlaybook{}, err
	}

	declared := make(map[string]Input, len(pb.RequiredInputs))
	for _, in := range pb.RequiredInputs {
		declared[in.Name] = in
	}
	for key := range inputs {
		if _, ok := declared[key]; !ok {
			return ResolvedPlaybook{}, fmt.Errorf("playbook %q: input %q is not declared", pb.ID, key)
		}
	}
	for _, in := range pb.RequiredInputs {
		if in.Required && strings.TrimSpace(inputs[in.Name]) == "" {
			return ResolvedPlaybook{}, fmt.Errorf("playbook %q: required input %q is missing", pb.ID, in.Name)
		}
	}

	calls := make([]ResolvedCall, 0, len(pb.Steps))
	for _, step := range pb.Steps {
		args, err := ResolveParams(pb.ID, step, inputs)
		if err != nil {
			return ResolvedPlaybook{}, err
		}
		calls = append(calls, ResolvedCall{
			StepID:           step.ID,
			Tool:             step.Tool,
			Arguments:        args,
			ExpectedTruth:    step.ExpectedTruth,
			EvidenceExpected: step.EvidenceExpected,
			Drilldowns:       step.Drilldowns,
		})
	}

	return ResolvedPlaybook{
		PlaybookID:   pb.ID,
		Version:      pb.Version,
		PromptFamily: pb.PromptFamily,
		Calls:        calls,
		FailureModes: pb.FailureModes,
	}, nil
}

// ResolveParams binds a step's declared params to concrete argument values. A
// from-input param is omitted when its optional input is absent; a required
// input is already enforced by Resolve before this point.
func ResolveParams(playbookID string, step Step, inputs map[string]string) (map[string]any, error) {
	args := make(map[string]any, len(step.Params))
	for _, param := range step.Params {
		switch param.source() {
		case ParamFromInput:
			value := strings.TrimSpace(inputs[param.FromInput])
			if value == "" {
				// Optional input not provided: omit the argument rather than
				// invent an empty value.
				continue
			}
			args[param.Name] = value
		case ParamConstInt:
			args[param.Name] = param.ConstInt
		case ParamConstBool:
			args[param.Name] = param.ConstBool
		case ParamConstString:
			args[param.Name] = param.ConstString
		default:
			return nil, fmt.Errorf("playbook %q: step %q param %q has no resolvable source", playbookID, step.ID, param.Name)
		}
	}
	return args, nil
}

// ToolNames returns the sorted, de-duplicated set of first-class tool
// names referenced by any step or drilldown across the catalog. It is exported
// so the mcp package can cross-check every name against the MCP tool registry
// (ReadOnlyTools) without the query package importing mcp, which would create an
// import cycle.
func ToolNames() []string {
	seen := make(map[string]struct{})
	for _, pb := range Catalog() {
		for _, step := range pb.Steps {
			seen[step.Tool] = struct{}{}
			for _, d := range step.Drilldowns {
				seen[d.Tool] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CatalogVersions returns the catalog identity as an ordered slice of
// (ID, Version) pairs in catalog declaration order. Tests pin this list so the
// catalog cannot drift silently.
func CatalogVersions() []VersionRef {
	catalog := Catalog()
	refs := make([]VersionRef, 0, len(catalog))
	for _, pb := range catalog {
		refs = append(refs, VersionRef{ID: pb.ID, Version: pb.Version})
	}
	return refs
}

// Lookup returns the catalog playbook with the given ID and whether it
// was found.
func Lookup(id string) (Definition, bool) {
	for _, pb := range Catalog() {
		if pb.ID == id {
			return pb, true
		}
	}
	return Definition{}, false
}
