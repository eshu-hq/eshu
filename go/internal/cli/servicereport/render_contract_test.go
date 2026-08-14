// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicereport

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// keyDisposition records what RenderReport does with the value at one JSON
// path of a marshalled serviceintel.Report.
type keyDisposition struct {
	// printed is true when RenderReport writes the value into the text form.
	printed bool
	// opaque marks a node whose entire subtree is absent from the text form.
	// Both walks stop at an opaque node, so a field added below it needs no
	// reclassification -- the claim "nothing under here is printed" already
	// covers it. Only ever set alongside printed=false.
	opaque bool
}

// renderKeyContract is the pinned classification the package docs describe in
// prose: which JSON keys of a serviceintel.Report reach the text form and
// which do not. The docs say the text prints a fixed subset and everything
// else is absent by design; this map is that sentence in a form a test can
// check.
//
// Array indices collapse to "[]", so one entry covers every element.
var renderKeyContract = map[string]keyDisposition{
	".schema":                                          {},
	".subject.service_name":                            {printed: true},
	".subject.service_id":                              {printed: true},
	".subject.repo_id":                                 {},
	".subject.repo_name":                               {},
	".supported":                                       {printed: true},
	".partial":                                         {printed: true},
	".truth_class":                                     {printed: true},
	".truth":                                           {opaque: true},
	".sections[].kind":                                 {},
	".sections[].title":                                {printed: true},
	".sections[].status":                               {printed: true},
	".sections[].answer.prompt_family":                 {},
	".sections[].answer.question":                      {},
	".sections[].answer.primary_tool":                  {},
	".sections[].answer.primary_route":                 {},
	".sections[].answer.truth_class":                   {},
	".sections[].answer.summary":                       {printed: true},
	".sections[].answer.supported":                     {},
	".sections[].answer.partial":                       {},
	".sections[].answer.result_ref":                    {},
	".sections[].answer.result":                        {opaque: true},
	".sections[].answer.truth":                         {opaque: true},
	".sections[].answer.limitations[]":                 {printed: true},
	".sections[].answer.truncated":                     {},
	".sections[].answer.missing_evidence":              {opaque: true},
	".sections[].answer.evidence_handles":              {opaque: true},
	".sections[].answer.citation_ref":                  {},
	".sections[].answer.recommended_next_calls":        {opaque: true},
	".sections[].answer.unsupported_reasons[]":         {printed: true},
	".limitations[]":                                   {},
	".recommended_next_calls[].tool":                   {printed: true},
	".recommended_next_calls[].route":                  {printed: true},
	".recommended_next_calls[].playbook":               {printed: true},
	".recommended_next_calls[].reason":                 {printed: true},
	".recommended_next_calls[].arguments":              {opaque: true},
	".suggested_investigations[].id":                   {},
	".suggested_investigations[].section":              {},
	".suggested_investigations[].basis":                {printed: true},
	".suggested_investigations[].reason":               {printed: true},
	".suggested_investigations[].evidence_basis[]":     {},
	".suggested_investigations[].next_call.tool":       {printed: true},
	".suggested_investigations[].next_call.route":      {printed: true},
	".suggested_investigations[].next_call.playbook":   {printed: true},
	".suggested_investigations[].next_call.reason":     {},
	".suggested_investigations[].next_call.arguments":  {opaque: true},
	".suggested_investigations[].expected_truth_class": {printed: true},
}

// unprobedKeys are the classified paths that carry no string value, so the
// render half cannot search the text for them: a bool renders as "true" or
// "false", which collides with unrelated text. They stay covered by the type
// walk, which proves they are still classified. The count is asserted so a
// new non-string field cannot quietly join this bucket.
var unprobedKeys = map[string]bool{
	".supported":                   true,
	".partial":                     true,
	".sections[].answer.supported": true,
	".sections[].answer.partial":   true,
	".sections[].answer.truncated": true,
}

// TestRenderReportJSONKeyContract is the mechanized form of the package docs'
// claim about the text output. doc.go, README.md, and AGENTS.md all state that
// RenderReport prints a fixed subset of serviceintel.Report and that
// everything else the JSON carries is absent by design. Hand-maintaining that
// list drifted repeatedly, so the list lives in renderKeyContract and this
// test checks it three ways:
//
//  1. shape -- walk the Go types behind serviceintel.Report and require every
//     JSON path to be classified. A field added upstream surfaces here as an
//     unclassified key even when it is omitempty and never marshals, which a
//     value-driven walk alone would miss.
//  2. render -- marshal a report whose every string leaf is a unique
//     sentinel, then assert each sentinel appears in RenderReport's output
//     exactly when its path is classified printed.
//  3. composed -- run the real ParseServiceStoryResponse -> FromServiceStory
//     -> Compose path and require every key it actually emits to be
//     classified, so the fixture cannot drift away from production output.
//
// Each phase asserts the number of keys it classified and refuses a zero
// count, because a loop over an empty set passes while proving nothing.
func TestRenderReportJSONKeyContract(t *testing.T) {
	t.Parallel()

	shape := walkReportType(reflect.TypeOf(serviceintel.Report{}), "")
	if len(shape) == 0 {
		t.Fatalf("the type walk found no JSON keys on serviceintel.Report; the walk is broken, not the contract")
	}
	for _, path := range sortedKeys(shape) {
		if _, ok := renderKeyContract[path]; !ok {
			t.Errorf("unclassified JSON key %q on serviceintel.Report: add it to renderKeyContract as printed (RenderReport writes it) or not printed (absent from the text form by design), and update doc.go, README.md, and AGENTS.md to match", path)
		}
	}
	for _, path := range sortedKeys(renderKeyContract) {
		if !shape[path] {
			t.Errorf("stale entry %q in renderKeyContract: no such JSON key exists on serviceintel.Report any more", path)
		}
	}
	if len(shape) != len(renderKeyContract) {
		t.Errorf("classified %d JSON keys from the report type, renderKeyContract holds %d", len(shape), len(renderKeyContract))
	}
	t.Logf("shape phase: classified %d JSON keys against a %d-entry contract", len(shape), len(renderKeyContract))

	report := sentinelReport()
	probes := flattenReportJSON(t, report)
	for _, path := range sortedKeys(probes) {
		if _, ok := renderKeyContract[path]; !ok {
			t.Errorf("unclassified JSON key %q in the marshalled sentinel report", path)
		}
	}
	for _, path := range sortedKeys(renderKeyContract) {
		if _, ok := probes[path]; !ok {
			t.Errorf("sentinelReport does not populate %q, so the render phase never checks it; give the field a sentinel value", path)
		}
	}

	var buf bytes.Buffer
	RenderReport(&buf, report)
	rendered := buf.String()

	checked, unprobed := 0, 0
	for _, path := range sortedKeys(probes) {
		values := probes[path]
		if len(values) == 0 {
			unprobed++
			if !unprobedKeys[path] {
				t.Errorf("JSON key %q carries no string value to search the text for; either give it a sentinel in sentinelReport or add it to unprobedKeys with a reason", path)
			}
			continue
		}
		want := renderKeyContract[path].printed
		for _, value := range values {
			got := strings.Contains(rendered, value)
			checked++
			switch {
			case want && !got:
				t.Errorf("JSON key %q is classified printed, but its value %q is missing from RenderReport's output:\n%s", path, value, rendered)
			case !want && got:
				t.Errorf("JSON key %q is classified absent from the text form, but its value %q appears in RenderReport's output; either RenderReport changed or the classification is wrong -- fix one, then update doc.go, README.md, and AGENTS.md:\n%s", path, value, rendered)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("the render phase asserted nothing; sentinelReport produced no string values")
	}
	if unprobed != len(unprobedKeys) {
		t.Errorf("render phase skipped %d keys for want of a string value, expected exactly %d (%v)", unprobed, len(unprobedKeys), sortedKeys(unprobedKeys))
	}
	t.Logf("render phase: asserted %d sentinel values across %d JSON keys, %d keys shape-only", checked, len(probes)-unprobed, unprobed)

	composed := flattenReportJSON(t, composeSampleReport(t))
	if len(composed) == 0 {
		t.Fatalf("the composed report marshalled to no JSON keys")
	}
	for _, path := range sortedKeys(composed) {
		if _, ok := renderKeyContract[path]; !ok {
			t.Errorf("unclassified JSON key %q emitted by a real composed report", path)
		}
	}
	t.Logf("composed phase: classified %d JSON keys emitted by serviceintel.Compose", len(composed))
}

// sentinelReport builds a serviceintel.Report with a unique sentinel in every
// string leaf renderKeyContract classifies. It is a struct literal rather than
// a Compose result because Compose derives most of these fields and cannot be
// steered into filling all of them at once; the composed phase of the test
// covers the real composer's output separately.
//
// The three next-call label fields (tool, route, playbook) are spread across
// three calls because nextCallLabel prints only the first one that is set.
func sentinelReport() serviceintel.Report {
	envelope := func(name string) *query.TruthEnvelope {
		return &query.TruthEnvelope{
			Level:     query.TruthLevel(sentinel(name + "-level")),
			Reason:    sentinel(name + "-reason"),
			Freshness: query.TruthFreshness{State: query.FreshnessState(sentinel(name + "-freshness"))},
		}
	}
	handle := func(name string) query.EvidenceCitationHandle {
		return query.EvidenceCitationHandle{
			Kind:           sentinel(name + "-kind"),
			RepoID:         sentinel(name + "-repo"),
			RelativePath:   sentinel(name + "-path"),
			EntityID:       sentinel(name + "-entity"),
			EvidenceFamily: sentinel(name + "-family"),
			Reason:         sentinel(name + "-reason"),
			StartLine:      1,
			EndLine:        2,
			Confidence:     0.5,
		}
	}

	return serviceintel.Report{
		Schema: sentinel("schema"),
		Subject: serviceintel.ReportSubject{
			ServiceName: sentinel("service-name"),
			ServiceID:   sentinel("service-id"),
			RepoID:      sentinel("subject-repo-id"),
			RepoName:    sentinel("subject-repo-name"),
		},
		Supported:  true,
		Partial:    true,
		TruthClass: query.AnswerTruthClass(sentinel("truth-class")),
		Truth:      envelope("report-truth"),
		Sections: []serviceintel.ReportSection{{
			Kind:  serviceintel.SectionKind(sentinel("section-kind")),
			Title: sentinel("section-title"),
			// RenderReport upper-cases the status, so the sentinel is already
			// upper-case and survives the round trip unchanged.
			Status: serviceintel.SectionStatus(strings.ToUpper(sentinel("section-status"))),
			Answer: query.AnswerPacket{
				PromptFamily:         sentinel("prompt-family"),
				Question:             sentinel("question"),
				PrimaryTool:          sentinel("primary-tool"),
				PrimaryRoute:         sentinel("primary-route"),
				TruthClass:           query.AnswerTruthClass(sentinel("answer-truth-class")),
				Summary:              sentinel("answer-summary"),
				Supported:            true,
				Partial:              true,
				ResultRef:            sentinel("result-ref"),
				Result:               sentinel("result"),
				Truth:                envelope("answer-truth"),
				Limitations:          []string{sentinel("answer-limitation")},
				Truncated:            true,
				MissingEvidence:      []query.EvidenceCitationHandle{handle("missing-evidence")},
				EvidenceHandles:      []query.EvidenceCitationHandle{handle("evidence-handle")},
				CitationRef:          sentinel("citation-ref"),
				RecommendedNextCalls: []map[string]any{{"tool": sentinel("answer-next-call")}},
				UnsupportedReasons:   []string{sentinel("unsupported-reason")},
			},
		}},
		Limitations: []string{sentinel("report-limitation")},
		NextCalls: []serviceintel.NextCall{
			{
				Tool:      sentinel("next-call-tool"),
				Reason:    sentinel("next-call-reason"),
				Arguments: map[string]any{"scope": sentinel("next-call-arguments")},
			},
			{Route: sentinel("next-call-route")},
			{Playbook: sentinel("next-call-playbook")},
		},
		Investigations: []serviceintel.SuggestedInvestigation{
			{
				ID:                 sentinel("investigation-id"),
				Section:            serviceintel.SectionKind(sentinel("investigation-section")),
				Basis:              serviceintel.InvestigationBasis(sentinel("investigation-basis")),
				Reason:             sentinel("investigation-reason"),
				EvidenceBasis:      []string{sentinel("investigation-evidence-basis")},
				ExpectedTruthClass: query.AnswerTruthClass(sentinel("investigation-truth-class")),
				NextCall: serviceintel.NextCall{
					Tool:      sentinel("investigation-next-call-tool"),
					Reason:    sentinel("investigation-next-call-reason"),
					Arguments: map[string]any{"scope": sentinel("investigation-next-call-arguments")},
				},
			},
			{NextCall: serviceintel.NextCall{Route: sentinel("investigation-next-call-route")}},
			{NextCall: serviceintel.NextCall{Playbook: sentinel("investigation-next-call-playbook")}},
		},
	}
}

// composeSampleReport runs the production path the CLI wrapper runs -- decode
// the captured response, adapt it, compose -- plus the optional supply-chain
// section, so the composed phase checks keys serviceintel actually emits
// rather than keys this test file invented.
func composeSampleReport(t *testing.T) serviceintel.Report {
	t.Helper()
	dossier, truth, err := ParseServiceStoryResponse([]byte(sampleServiceStoryEnvelope))
	if err != nil {
		t.Fatalf("parse sample service-story envelope: %v", err)
	}
	input := serviceintel.FromServiceStory(dossier, truth)

	path := fmt.Sprintf("%s/supply-chain.json", t.TempDir())
	writeFile(t, path, sampleSupplyChainEnvelope)
	section, err := SupplyChainSection(path, input.Subject)
	if err != nil {
		t.Fatalf("read sample supply-chain inventory: %v", err)
	}
	if section != nil {
		input.Sections = append(input.Sections, *section)
	}
	return serviceintel.Compose(input)
}

// TestNextCallLabelPrecedence pins which locator wins when one NextCall
// carries more than one, and pins the "playbook:" prefix.
//
// TestRenderReportJSONKeyContract cannot cover this. Its fixture gives each
// locator its own call, so every sentinel appears in the output and the
// contract passes whichever locator nextCallLabel picks. Reversing the
// precedence would keep that test green and still change what operators read,
// because production emits calls carrying several locators at once --
// serviceintel's section fallbacks set Tool, Route and Playbook together
// (go/internal/serviceintel/sections.go), so the choice is load-bearing, not
// theoretical.
//
// The losing locators are asserted absent, not merely the winner present. A
// test that only checked the winner would pass if RenderReport printed all
// three.
func TestNextCallLabelPrecedence(t *testing.T) {
	t.Parallel()

	const (
		tool     = "PRECEDENCE-tool-ZZ"
		route    = "PRECEDENCE-route-ZZ"
		playbook = "PRECEDENCE-playbook-ZZ"
	)

	cases := []struct {
		name   string
		call   serviceintel.NextCall
		want   string
		absent []string
	}{
		{
			name:   "tool beats route and playbook",
			call:   serviceintel.NextCall{Tool: tool, Route: route, Playbook: playbook},
			want:   tool,
			absent: []string{route, playbook},
		},
		{
			name:   "route beats playbook when tool is empty",
			call:   serviceintel.NextCall{Route: route, Playbook: playbook},
			want:   route,
			absent: []string{playbook},
		},
		{
			name: "playbook renders with its prefix when it is the only locator",
			call: serviceintel.NextCall{Playbook: playbook},
			want: "playbook:" + playbook,
		},
	}

	for _, tc := range cases {
		t.Run("next_calls/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			RenderReport(&buf, serviceintel.Report{NextCalls: []serviceintel.NextCall{tc.call}})
			assertLabel(t, buf.String(), tc.want, tc.absent)
		})

		t.Run("investigation/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			RenderReport(&buf, serviceintel.Report{
				Investigations: []serviceintel.SuggestedInvestigation{{NextCall: tc.call}},
			})
			assertLabel(t, buf.String(), tc.want, tc.absent)
		})
	}
}

// assertLabel checks that the winning locator reached the text and that every
// losing locator did not.
func assertLabel(t *testing.T, rendered, want string, absent []string) {
	t.Helper()
	if !strings.Contains(rendered, want) {
		t.Errorf("rendered text is missing the winning locator %q:\n%s", want, rendered)
	}
	for _, loser := range absent {
		if strings.Contains(rendered, loser) {
			t.Errorf("rendered text contains %q, which nextCallLabel's documented precedence says loses to %q; either the precedence changed or AGENTS.md is wrong -- fix one:\n%s", loser, want, rendered)
		}
	}
}
