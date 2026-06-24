// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMarksPyPIParserImportReachable(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi-reachable", "CVE-2026-118601", 8.7),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-reachable",
			"CVE-2026-118601",
			"pkg:pypi/requests",
			"pypi",
			"requests",
			"2.32.0",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-reachable",
			"pkg:pypi/requests",
			testImpactRepositoryID,
			"2.31.0",
			[]string{"requests"},
			1,
			true,
		),
		pythonReachabilityFileFact(
			"file-pypi-reachable",
			testImpactRepositoryID,
			"app.py",
			[]any{map[string]any{
				"name":        "requests",
				"source":      "requests",
				"import_type": "import",
				"lang":        "python",
			}},
			[]any{map[string]any{
				"name":        "get",
				"full_name":   "requests.get",
				"line_number": 7,
				"lang":        "python",
			}},
			nil,
			nil,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118601"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "python_parser_call" {
		t.Fatalf("RuntimeReachability = %q, want python_parser_call", got.RuntimeReachability)
	}
	if got.Reachability == nil || got.Reachability.State != SupplyChainReachabilityReachable {
		t.Fatalf("Reachability = %#v, want reachable envelope", got.Reachability)
	}
	if got.Reachability.Source != "python_parser" {
		t.Fatalf("Reachability.Source = %q, want python_parser", got.Reachability.Source)
	}
	if got.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want impact truth preserved", got.Status)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsPyPIDynamicImportsAmbiguous(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi-dynamic", "CVE-2026-118602", 7.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-dynamic",
			"CVE-2026-118602",
			"pkg:pypi/friendly-bard-plugin",
			"pypi",
			"friendly-bard-plugin",
			"1.2.0",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-dynamic",
			"pkg:pypi/friendly-bard-plugin",
			testImpactRepositoryID,
			"1.1.0",
			[]string{"friendly-bard-plugin"},
			1,
			true,
		),
		pythonReachabilityFileFact(
			"file-pypi-dynamic",
			testImpactRepositoryID,
			"plugins.py",
			nil,
			[]any{
				map[string]any{
					"name":        "import_module",
					"full_name":   "importlib.import_module",
					"line_number": 11,
					"lang":        "python",
				},
				map[string]any{
					"name":        "entry_points",
					"full_name":   "importlib.metadata.entry_points",
					"line_number": 12,
					"lang":        "python",
				},
			},
			nil,
			nil,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118602"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.Reachability == nil || got.Reachability.State != SupplyChainReachabilityUnknown {
		t.Fatalf("Reachability = %#v, want unknown for dynamic/plugin evidence", got.Reachability)
	}
	for _, want := range []string{
		"python package API identity missing",
		"python dynamic import evidence ambiguous",
		"python plugin loading evidence ambiguous",
	} {
		if !stringSliceContains(got.Reachability.MissingEvidence, want) {
			t.Fatalf("Reachability.MissingEvidence = %#v, want %q", got.Reachability.MissingEvidence, want)
		}
	}
	if stringSliceContains(got.PriorityReasonCodes, "reachability_not_called") {
		t.Fatalf("PriorityReasonCodes = %#v, must not infer not_called for Python parser gaps", got.PriorityReasonCodes)
	}
}

func TestBuildSupplyChainImpactFindingsMarksPyPISCIPCallReachable(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-pypi-scip", "CVE-2026-118604", 8.2),
		vulnerabilityAffectedPackageRangeFact(
			"affected-pypi-scip",
			"CVE-2026-118604",
			"pkg:pypi/django",
			"pypi",
			"django",
			"4.2.20",
		),
		packageConsumptionFactWithChain(
			"consume-pypi-scip",
			"pkg:pypi/django",
			testImpactRepositoryID,
			"4.2.19",
			[]string{"django"},
			1,
			true,
		),
		pythonReachabilityFileFact(
			"file-pypi-scip",
			testImpactRepositoryID,
			"views.py",
			nil,
			nil,
			nil,
			[]any{map[string]any{
				"callee_symbol": "scip-python python django/views#View.as_view().",
			}},
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118604"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "python_scip_call" {
		t.Fatalf("RuntimeReachability = %q, want python_scip_call", got.RuntimeReachability)
	}
	if got.Reachability == nil || got.Reachability.Source != "python_scip" {
		t.Fatalf("Reachability = %#v, want python_scip source", got.Reachability)
	}
}

func TestSupplyChainImpactHandlerLoadsPyPIParserEvidenceByRepository(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repository := packageSourceRepositoryFact(
		testImpactRepositoryID,
		"api",
		"https://github.com/acme/api",
		false,
		observedAt,
	)
	repository.ScopeID = "git-repository-scope:repository:r_api"
	repository.GenerationID = "git-generation-api-active"
	loader := &pythonReachabilityImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-pypi-handler", "CVE-2026-118603", 8.7),
			vulnerabilityAffectedPackageRangeFact(
				"affected-pypi-handler",
				"CVE-2026-118603",
				"pkg:pypi/requests",
				"pypi",
				"requests",
				"2.32.0",
			),
			packageConsumptionFactWithChain(
				"consume-pypi-handler",
				"pkg:pypi/requests",
				testImpactRepositoryID,
				"2.31.0",
				[]string{"requests"},
				1,
				true,
			),
		},
		repositoryFacts: []facts.Envelope{repository},
		payloadFacts: []facts.Envelope{
			pythonReachabilityFileFact(
				"file-pypi-handler-unrelated",
				testImpactRepositoryID,
				"other.py",
				[]any{map[string]any{"name": "flask", "source": "flask", "lang": "python"}},
				nil,
				nil,
				nil,
			),
			pythonReachabilityFileFact(
				"file-pypi-handler",
				testImpactRepositoryID,
				"app.py",
				[]any{map[string]any{"name": "requests", "source": "requests", "lang": "python"}},
				[]any{map[string]any{"name": "get", "full_name": "requests.get", "line_number": 4, "lang": "python"}},
				nil,
				nil,
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-pypi-reachability",
		ScopeID:      "scope-pypi-reachability",
		GenerationID: "generation-pypi-reachability",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(loader.payloadCalls), 1; got != want {
		t.Fatalf("ListFactsByKindAndPayloadValue() calls = %d, want %d", got, want)
	}
	call := loader.payloadCalls[0]
	if call.factKind != factKindFile || call.payloadKey != "repo_id" {
		t.Fatalf("payload call = %#v, want file facts by repo_id", call)
	}
	if call.scopeID != repository.ScopeID || call.generationID != repository.GenerationID {
		t.Fatalf(
			"payload call scope = (%q, %q), want repository scope (%q, %q)",
			call.scopeID,
			call.generationID,
			repository.ScopeID,
			repository.GenerationID,
		)
	}
	if got, want := strings.Join(call.payloadValues, ","), testImpactRepositoryID; got != want {
		t.Fatalf("payload values = %q, want %q", got, want)
	}
	if len(writer.write.Findings) != 1 {
		t.Fatalf("written findings = %d, want 1", len(writer.write.Findings))
	}
	if got := writer.write.Findings[0].Reachability; got == nil || got.State != SupplyChainReachabilityReachable {
		t.Fatalf("written Reachability = %#v, want reachable", got)
	}
	if stringSliceContains(writer.write.Findings[0].EvidenceFactIDs, "file-pypi-handler-unrelated") {
		t.Fatalf("EvidenceFactIDs = %#v, must only cite matched Python file facts", writer.write.Findings[0].EvidenceFactIDs)
	}
}

func TestSupplyChainImpactHandlerKeepsPyPIReachabilityMissingWithoutRepositoryScope(t *testing.T) {
	t.Parallel()

	loader := &pythonReachabilityImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-pypi-missing-repo-scope", "CVE-2026-118605", 8.1),
			vulnerabilityAffectedPackageRangeFact(
				"affected-pypi-missing-repo-scope",
				"CVE-2026-118605",
				"pkg:pypi/requests",
				"pypi",
				"requests",
				"2.32.0",
			),
			packageConsumptionFactWithChain(
				"consume-pypi-missing-repo-scope",
				"pkg:pypi/requests",
				testImpactRepositoryID,
				"2.31.0",
				[]string{"requests"},
				1,
				true,
			),
		},
		payloadFacts: []facts.Envelope{
			pythonReachabilityFileFact(
				"file-pypi-missing-repo-scope",
				testImpactRepositoryID,
				"app.py",
				[]any{map[string]any{"name": "requests", "source": "requests", "lang": "python"}},
				[]any{map[string]any{"name": "get", "full_name": "requests.get", "line_number": 4, "lang": "python"}},
				nil,
				nil,
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-pypi-missing-repo-scope",
		ScopeID:      "vuln-intel://osv/pypi/requests@2.32.0",
		GenerationID: "generation-vuln-intel-pypi-requests",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(loader.payloadCalls) != 0 {
		t.Fatalf("ListFactsByKindAndPayloadValue() calls = %d, want 0 without repository scope", len(loader.payloadCalls))
	}
	if len(writer.write.Findings) != 1 {
		t.Fatalf("written findings = %d, want 1", len(writer.write.Findings))
	}
	finding := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
	if got := finding.Reachability; got == nil || got.State != SupplyChainReachabilityMissingEvidence {
		t.Fatalf("written Reachability = %#v, want missing evidence", got)
	}
	if !stringSliceContains(finding.Reachability.MissingEvidence, "python parser or SCIP reachability evidence missing") {
		t.Fatalf("MissingEvidence = %#v, want parser evidence missing", finding.Reachability.MissingEvidence)
	}
	if stringSliceContains(finding.EvidenceFactIDs, "file-pypi-missing-repo-scope") {
		t.Fatalf("EvidenceFactIDs = %#v, must not cite unqueried repository file facts", finding.EvidenceFactIDs)
	}
}

type pythonReachabilityImpactLoader struct {
	scopeFacts      []facts.Envelope
	repositoryFacts []facts.Envelope
	payloadFacts    []facts.Envelope
	kindCalls       [][]string
	repositoryCalls int
	manifestCalls   int
	payloadCalls    []pythonPayloadFactCall
}

type pythonPayloadFactCall struct {
	scopeID       string
	generationID  string
	factKind      string
	payloadKey    string
	payloadValues []string
}

func (l *pythonReachabilityImpactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *pythonReachabilityImpactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	l.kindCalls = append(l.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (l *pythonReachabilityImpactLoader) ListFactsByKindAndPayloadValue(
	_ context.Context,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	l.payloadCalls = append(l.payloadCalls, pythonPayloadFactCall{
		scopeID:       scopeID,
		generationID:  generationID,
		factKind:      factKind,
		payloadKey:    payloadKey,
		payloadValues: append([]string(nil), payloadValues...),
	})
	if scopeID != "git-repository-scope:repository:r_api" || generationID != "git-generation-api-active" {
		return nil, nil
	}
	return append([]facts.Envelope(nil), l.payloadFacts...), nil
}

func (l *pythonReachabilityImpactLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	l.repositoryCalls++
	return append([]facts.Envelope(nil), l.repositoryFacts...), nil
}

func (l *pythonReachabilityImpactLoader) ListActivePackageManifestDependencyFacts(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, error) {
	l.manifestCalls++
	return nil, nil
}

func (l *pythonReachabilityImpactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	return nil, nil
}

func pythonReachabilityFileFact(
	factID string,
	repositoryID string,
	relativePath string,
	imports []any,
	functionCalls []any,
	functions []any,
	scipCalls []any,
) facts.Envelope {
	fileData := map[string]any{
		"path": relativePath,
	}
	if imports != nil {
		fileData["imports"] = imports
	}
	if functionCalls != nil {
		fileData["function_calls"] = functionCalls
	}
	if functions != nil {
		fileData["functions"] = functions
	}
	if scipCalls != nil {
		fileData["function_calls_scip"] = scipCalls
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: factKindFile,
		SourceRef: facts.Ref{
			SourceSystem: "git",
		},
		Payload: map[string]any{
			"repo_id":          repositoryID,
			"relative_path":    relativePath,
			"parsed_file_data": fileData,
		},
	}
}
