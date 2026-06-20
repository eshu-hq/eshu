package query

import (
	"fmt"
	"sort"
	"strings"
)

// CapabilityInvestigationWorkflows identifies deterministic guided
// investigation workflow catalog and resolver reads. The capability returns
// workflow-plan truth, not live graph query truth.
const CapabilityInvestigationWorkflows = "query.investigation_workflows"

// WorkflowEvidence declares an evidence family a guided investigation expects.
type WorkflowEvidence struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	SourceFamilies []string `json:"source_families,omitempty"`
}

// WorkflowOutputPacket describes the bounded packet a workflow is expected to
// produce after the recommended calls have been executed by a caller.
type WorkflowOutputPacket struct {
	Schema      string   `json:"schema"`
	TruthLabels []string `json:"truth_labels"`
	Sections    []string `json:"sections"`
}

// WorkflowFailureMode documents an expected missing, stale, hidden, or refused
// state and the bounded caller action that preserves truth instead of guessing.
type WorkflowFailureMode struct {
	Condition         string   `json:"condition"`
	Meaning           string   `json:"meaning"`
	RecommendedAction string   `json:"recommended_action"`
	RelatedEvidence   []string `json:"related_evidence,omitempty"`
}

// WorkflowToolGroup groups existing atomic tools behind a high-level workflow
// so assistants start from the workflow without losing expert tool discovery.
type WorkflowToolGroup struct {
	Name  string   `json:"name"`
	Tools []string `json:"tools"`
}

// WorkflowNextCall is one bounded follow-up call selected by missing evidence.
type WorkflowNextCall struct {
	ID                string          `json:"id"`
	Tool              string          `json:"tool"`
	Reason            string          `json:"reason"`
	MissingEvidence   string          `json:"missing_evidence"`
	Params            []PlaybookParam `json:"params,omitempty"`
	RequiredInputsAny []string        `json:"required_inputs_any,omitempty"`
	ExpectedEvidence  string          `json:"expected_evidence"`
}

// WorkflowMissingEvidenceRoute maps an observed missing-evidence key to the
// bounded calls that can gather or explain that missing evidence.
type WorkflowMissingEvidenceRoute struct {
	EvidenceKey string             `json:"evidence_key"`
	States      []string           `json:"states,omitempty"`
	Calls       []WorkflowNextCall `json:"calls"`
}

// InvestigationWorkflow is a deterministic, versioned description of a guided
// investigation over first-class API/MCP tools. It reads no live state: callers
// provide inputs plus observed missing-evidence keys, and Resolve returns the
// bounded next calls for those states.
type InvestigationWorkflow struct {
	ID                    string                         `json:"id"`
	Name                  string                         `json:"name"`
	Version               string                         `json:"version"`
	Domain                string                         `json:"domain"`
	Description           string                         `json:"description"`
	RequiredInputs        []PlaybookInput                `json:"required_inputs"`
	RequiredEvidence      []WorkflowEvidence             `json:"required_evidence"`
	OptionalEvidence      []WorkflowEvidence             `json:"optional_evidence"`
	OutputPacket          WorkflowOutputPacket           `json:"output_packet"`
	ToolGroups            []WorkflowToolGroup            `json:"tool_groups"`
	StarterPrompts        []string                       `json:"starter_prompts"`
	FailureModes          []WorkflowFailureMode          `json:"failure_modes,omitempty"`
	MissingEvidenceRoutes []WorkflowMissingEvidenceRoute `json:"missing_evidence_routes"`
}

// InvestigationWorkflowVersionRef is a compact (ID, Version) pair for catalog
// stability assertions and list responses.
type InvestigationWorkflowVersionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// InvestigationWorkflowResolveInput is the caller-supplied state used to turn a
// workflow into concrete recommended next calls.
type InvestigationWorkflowResolveInput struct {
	Inputs          map[string]string `json:"inputs"`
	MissingEvidence []string          `json:"missing_evidence"`
}

// ResolvedWorkflowCall is one concrete next call produced by Resolve.
type ResolvedWorkflowCall struct {
	ID               string         `json:"id"`
	Tool             string         `json:"tool"`
	Reason           string         `json:"reason"`
	MissingEvidence  string         `json:"missing_evidence"`
	Arguments        map[string]any `json:"arguments"`
	ExpectedEvidence string         `json:"expected_evidence"`
}

// BlockedWorkflowCall names a bounded next call that was not recommended
// because none of its required anchor inputs were supplied by the caller.
type BlockedWorkflowCall struct {
	ID                string   `json:"id"`
	Tool              string   `json:"tool"`
	Reason            string   `json:"reason"`
	MissingEvidence   string   `json:"missing_evidence"`
	RequiredInputsAny []string `json:"required_inputs_any"`
}

// ResolvedInvestigationWorkflow is the deterministic resolver output for one
// workflow and caller-provided missing-evidence state.
type ResolvedInvestigationWorkflow struct {
	WorkflowID               string                 `json:"workflow_id"`
	Version                  string                 `json:"version"`
	Domain                   string                 `json:"domain"`
	OutputPacket             WorkflowOutputPacket   `json:"output_packet"`
	RecommendedNextCalls     []ResolvedWorkflowCall `json:"recommended_next_calls"`
	BlockedNextCalls         []BlockedWorkflowCall  `json:"blocked_next_calls,omitempty"`
	UnmatchedMissingEvidence []string               `json:"unmatched_missing_evidence,omitempty"`
}

// InvestigationWorkflowCatalog returns the versioned catalog of guided
// investigation workflows. The catalog is static and deterministic; it does not
// execute tools or read graph, Postgres, providers, collectors, or tenant data.
func InvestigationWorkflowCatalog() []InvestigationWorkflow {
	return []InvestigationWorkflow{
		vulnerableDependencyWorkflow(),
		deployableDriftWorkflow(),
		incidentContextWorkflow(),
	}
}

// InvestigationWorkflowCatalogVersions returns ordered (ID, Version) refs.
func InvestigationWorkflowCatalogVersions() []InvestigationWorkflowVersionRef {
	catalog := InvestigationWorkflowCatalog()
	refs := make([]InvestigationWorkflowVersionRef, 0, len(catalog))
	for _, workflow := range catalog {
		refs = append(refs, InvestigationWorkflowVersionRef{ID: workflow.ID, Version: workflow.Version})
	}
	return refs
}

// InvestigationWorkflowToolNames returns the sorted, de-duplicated set of
// first-class tool names referenced by workflow tool groups and missing-evidence
// next calls. It lets the MCP package verify catalog references against the
// authoritative read-only tool registry without creating an import cycle.
func InvestigationWorkflowToolNames() []string {
	seen := make(map[string]struct{})
	for _, workflow := range InvestigationWorkflowCatalog() {
		for _, group := range workflow.ToolGroups {
			for _, tool := range group.Tools {
				seen[tool] = struct{}{}
			}
		}
		for _, route := range workflow.MissingEvidenceRoutes {
			for _, call := range route.Calls {
				seen[call.Tool] = struct{}{}
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

// LookupInvestigationWorkflow returns the workflow with the given ID.
func LookupInvestigationWorkflow(id string) (InvestigationWorkflow, bool) {
	for _, workflow := range InvestigationWorkflowCatalog() {
		if workflow.ID == id {
			return workflow, true
		}
	}
	return InvestigationWorkflow{}, false
}

// Resolve turns caller-provided inputs and observed missing-evidence keys into
// bounded next calls. It is deterministic and performs no I/O.
func (w InvestigationWorkflow) Resolve(input InvestigationWorkflowResolveInput) (ResolvedInvestigationWorkflow, error) {
	if err := w.Validate(); err != nil {
		return ResolvedInvestigationWorkflow{}, err
	}
	if err := validateWorkflowInputs(w, input.Inputs); err != nil {
		return ResolvedInvestigationWorkflow{}, err
	}

	missing := normalizeWorkflowEvidence(input.MissingEvidence)
	calls := make([]ResolvedWorkflowCall, 0)
	blocked := make([]BlockedWorkflowCall, 0)
	matched := make(map[string]struct{}, len(missing))
	for _, route := range w.MissingEvidenceRoutes {
		key := normalizeWorkflowKey(route.EvidenceKey)
		if _, ok := missing[key]; !ok {
			continue
		}
		matched[key] = struct{}{}
		for _, call := range route.Calls {
			if len(call.RequiredInputsAny) > 0 && !hasAnyWorkflowInput(input.Inputs, call.RequiredInputsAny) {
				blocked = append(blocked, BlockedWorkflowCall{
					ID:                call.ID,
					Tool:              call.Tool,
					Reason:            call.Reason,
					MissingEvidence:   route.EvidenceKey,
					RequiredInputsAny: append([]string(nil), call.RequiredInputsAny...),
				})
				continue
			}
			args, err := resolveWorkflowCallArgs(w.ID, call, input.Inputs)
			if err != nil {
				return ResolvedInvestigationWorkflow{}, err
			}
			calls = append(calls, ResolvedWorkflowCall{
				ID:               call.ID,
				Tool:             call.Tool,
				Reason:           call.Reason,
				MissingEvidence:  route.EvidenceKey,
				Arguments:        args,
				ExpectedEvidence: call.ExpectedEvidence,
			})
		}
	}

	unmatched := make([]string, 0)
	for key := range missing {
		if _, ok := matched[key]; !ok {
			unmatched = append(unmatched, key)
		}
	}
	sort.Strings(unmatched)

	return ResolvedInvestigationWorkflow{
		WorkflowID:               w.ID,
		Version:                  w.Version,
		Domain:                   w.Domain,
		OutputPacket:             w.OutputPacket,
		RecommendedNextCalls:     calls,
		BlockedNextCalls:         blocked,
		UnmatchedMissingEvidence: unmatched,
	}, nil
}

// Validate checks the static workflow authoring contract.
func (w InvestigationWorkflow) Validate() error {
	switch {
	case strings.TrimSpace(w.ID) == "":
		return fmt.Errorf("workflow ID is required")
	case strings.TrimSpace(w.Name) == "":
		return fmt.Errorf("workflow %q: name is required", w.ID)
	case strings.TrimSpace(w.Version) == "":
		return fmt.Errorf("workflow %q: version is required", w.ID)
	case strings.TrimSpace(w.Domain) == "":
		return fmt.Errorf("workflow %q: domain is required", w.ID)
	case strings.TrimSpace(w.OutputPacket.Schema) == "":
		return fmt.Errorf("workflow %q: output packet schema is required", w.ID)
	case len(w.RequiredEvidence) == 0:
		return fmt.Errorf("workflow %q: required evidence is required", w.ID)
	case len(w.ToolGroups) == 0:
		return fmt.Errorf("workflow %q: tool groups are required", w.ID)
	case len(w.StarterPrompts) == 0:
		return fmt.Errorf("workflow %q: starter prompts are required", w.ID)
	case len(w.MissingEvidenceRoutes) == 0:
		return fmt.Errorf("workflow %q: missing evidence routes are required", w.ID)
	}
	declared, err := validateWorkflowDeclaredInputs(w)
	if err != nil {
		return err
	}
	for _, group := range w.ToolGroups {
		for _, tool := range group.Tools {
			if isRawCypherTool(tool) {
				return fmt.Errorf("workflow %q: tool group %q references raw query tool %q", w.ID, group.Name, tool)
			}
		}
	}
	for _, mode := range w.FailureModes {
		if strings.TrimSpace(mode.Condition) == "" {
			return fmt.Errorf("workflow %q: failure mode condition is required", w.ID)
		}
		if strings.TrimSpace(mode.Meaning) == "" {
			return fmt.Errorf("workflow %q: failure mode %q meaning is required", w.ID, mode.Condition)
		}
		if strings.TrimSpace(mode.RecommendedAction) == "" {
			return fmt.Errorf("workflow %q: failure mode %q recommended action is required", w.ID, mode.Condition)
		}
	}
	for _, route := range w.MissingEvidenceRoutes {
		if strings.TrimSpace(route.EvidenceKey) == "" {
			return fmt.Errorf("workflow %q: missing evidence route key is required", w.ID)
		}
		if len(route.Calls) == 0 {
			return fmt.Errorf("workflow %q: missing evidence route %q has no calls", w.ID, route.EvidenceKey)
		}
		for _, call := range route.Calls {
			if err := validateWorkflowCall(w.ID, call, declared); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateWorkflowDeclaredInputs(w InvestigationWorkflow) (map[string]PlaybookInput, error) {
	declared := make(map[string]PlaybookInput, len(w.RequiredInputs))
	for _, input := range w.RequiredInputs {
		if strings.TrimSpace(input.Name) == "" {
			return nil, fmt.Errorf("workflow %q: input name is required", w.ID)
		}
		if input.Type != PlaybookInputString && input.Type != PlaybookInputIdentifier {
			return nil, fmt.Errorf("workflow %q: input %q has unknown type %q", w.ID, input.Name, input.Type)
		}
		if _, exists := declared[input.Name]; exists {
			return nil, fmt.Errorf("workflow %q: duplicate input %q", w.ID, input.Name)
		}
		declared[input.Name] = input
	}
	return declared, nil
}

func validateWorkflowInputs(w InvestigationWorkflow, inputs map[string]string) error {
	declared, err := validateWorkflowDeclaredInputs(w)
	if err != nil {
		return err
	}
	for key := range inputs {
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("workflow %q: input %q is not declared", w.ID, key)
		}
	}
	for _, input := range declared {
		if input.Required && strings.TrimSpace(inputs[input.Name]) == "" {
			return fmt.Errorf("workflow %q: required input %q is missing", w.ID, input.Name)
		}
	}
	return nil
}

func validateWorkflowCall(workflowID string, call WorkflowNextCall, declared map[string]PlaybookInput) error {
	if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Tool) == "" {
		return fmt.Errorf("workflow %q: next call must declare id and tool", workflowID)
	}
	if isRawCypherTool(call.Tool) {
		return fmt.Errorf("workflow %q: next call %q references raw query tool %q", workflowID, call.ID, call.Tool)
	}
	for _, param := range call.Params {
		if strings.TrimSpace(param.Name) == "" {
			return fmt.Errorf("workflow %q: next call %q has an unnamed param", workflowID, call.ID)
		}
		if err := param.validateSingleSource(workflowID, call.ID); err != nil {
			return err
		}
		if param.FromInput != "" {
			if _, ok := declared[param.FromInput]; !ok {
				return fmt.Errorf("workflow %q: next call %q param %q references undeclared input %q", workflowID, call.ID, param.Name, param.FromInput)
			}
		}
	}
	for _, inputName := range call.RequiredInputsAny {
		if _, ok := declared[inputName]; !ok {
			return fmt.Errorf("workflow %q: next call %q required input %q is not declared", workflowID, call.ID, inputName)
		}
	}
	return nil
}

func resolveWorkflowCallArgs(workflowID string, call WorkflowNextCall, inputs map[string]string) (map[string]any, error) {
	return resolveParams(workflowID, PlaybookStep{ID: call.ID, Params: call.Params}, inputs)
}

func normalizeWorkflowEvidence(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := normalizeWorkflowKey(value)
		if key != "" {
			result[key] = struct{}{}
		}
	}
	return result
}

func normalizeWorkflowKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func hasAnyWorkflowInput(inputs map[string]string, names []string) bool {
	for _, name := range names {
		if strings.TrimSpace(inputs[name]) != "" {
			return true
		}
	}
	return false
}
