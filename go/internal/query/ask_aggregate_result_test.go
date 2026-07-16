// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"testing"
)

const indexedRepositoriesResultRef = "eshu://api-result/repositories"

func exactIndexedRepositoriesAnswer(total int64) AskAnswer {
	return AskAnswer{
		Packets: []AnswerPacket{{
			PrimaryTool: indexedRepositoryInventoryToolForTest,
			TruthClass:  AnswerTruthDeterministic,
			Summary:     "authoritative indexed repository count",
			Supported:   true,
			ResultRef:   indexedRepositoriesResultRef,
			Result:      map[string]any{"total": total},
		}},
		Trace: []AskTraceEntry{{
			Tool:       indexedRepositoryInventoryToolForTest,
			Supported:  true,
			TruthClass: AnswerTruthDeterministic,
		}},
	}
}

const indexedRepositoryInventoryToolForTest = "list_indexed_repositories"

func TestBuildAskResponseSurfacesAggregateResultReference(t *testing.T) {
	t.Parallel()

	response := buildAskResponse(exactIndexedRepositoriesAnswer(896), "How many repositories are currently indexed?", "")
	assertAskAggregateResult(t, response, 896)
}

func TestAskHTTPResponseSurfacesAggregateResultReference(t *testing.T) {
	t.Parallel()

	handler := &AskHandler{Asker: &fakeAsker{answer: exactIndexedRepositoriesAnswer(896)}}
	recorder := postAsk(handler, `{"question":"How many repositories are currently indexed?"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		ResultRef  string         `json:"result_ref"`
		Result     map[string]any `json:"result"`
		TruthClass string         `json:"truth_class"`
		QueryTrace []traceEntry   `json:"query_trace"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ResultRef != indexedRepositoriesResultRef {
		t.Fatalf("result_ref = %q, want %q", response.ResultRef, indexedRepositoriesResultRef)
	}
	if got := response.Result["total"]; got != float64(896) {
		t.Fatalf("result total = %#v, want 896", got)
	}
	if response.TruthClass != string(AnswerTruthDeterministic) {
		t.Fatalf("truth_class = %q, want %q", response.TruthClass, AnswerTruthDeterministic)
	}
	if len(response.QueryTrace) != 1 || response.QueryTrace[0].Tool != indexedRepositoryInventoryToolForTest || !response.QueryTrace[0].Supported {
		t.Fatalf("query_trace = %#v, want supported %s", response.QueryTrace, indexedRepositoryInventoryToolForTest)
	}
}

func TestAskSSEStreamingAnswerSurfacesAggregateResultReference(t *testing.T) {
	t.Parallel()

	handler := &AskHandler{
		Asker: &fakeStreamingAsker{answer: exactIndexedRepositoriesAnswer(896)},
	}
	recorder := postAskSSE(handler, `{"question":"How many repositories are currently indexed?"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var answer askResponse
	for _, event := range parseSSEEvents(recorder.Body.String()) {
		if event.event != "answer" {
			continue
		}
		if err := json.Unmarshal([]byte(event.data), &answer); err != nil {
			t.Fatalf("decode answer event: %v", err)
		}
	}
	assertAskAggregateResult(t, answer, 896)
}

func TestBuildAskResponseUsesExplicitInventoryPacketAcrossMultiPacketAnswer(t *testing.T) {
	t.Parallel()

	response := buildAskResponse(multiPacketExactIndexedRepositoriesAnswer(896), "How many repositories are currently indexed?", "")
	assertAskInventoryPublication(t, response)
}

func TestBuildAskResponseInvalidPrimaryPacketIndexFallsBackSafely(t *testing.T) {
	t.Parallel()

	answer := multiPacketExactIndexedRepositoriesAnswer(896)
	invalidIndex := len(answer.Packets)
	answer.PrimaryPacketIndex = &invalidIndex
	response := buildAskResponse(answer, "How many repositories are currently indexed?", "")

	if got, want := response.ResultRef, "eshu://api-result/collectors"; got != want {
		t.Fatalf("result_ref = %q, want first-supported fallback %q", got, want)
	}
}

func TestBuildAskResponseDoesNotFallbackFromSelectedUnavailableInventory(t *testing.T) {
	t.Parallel()

	answer := multiPacketExactIndexedRepositoriesAnswer(896)
	answer.Packets[1].Supported = false
	answer.Packets[1].TruthClass = AnswerTruthUnsupported
	answer.Packets[1].Summary = ""
	answer.Packets[1].ResultRef = ""
	answer.Packets[1].Result = nil
	response := buildAskResponse(answer, "How many repositories are currently indexed?", "")

	if response.AnswerProse != "" {
		t.Fatalf("answer_prose = %q, want empty unavailable inventory answer", response.AnswerProse)
	}
	if response.ResultRef != "" || response.Result != nil {
		t.Fatalf("result = (%q, %#v), want no unrelated fallback", response.ResultRef, response.Result)
	}
	if got, want := response.TruthClass, string(AnswerTruthUnsupported); got != want {
		t.Fatalf("truth_class = %q, want %q", got, want)
	}
}

func TestAskHTTPAndSSEUseExplicitInventoryPacketAcrossMultiPacketAnswer(t *testing.T) {
	t.Parallel()

	answer := multiPacketExactIndexedRepositoriesAnswer(896)
	httpRecorder := postAsk(&AskHandler{Asker: &fakeAsker{answer: answer}}, `{"question":"How many repositories are currently indexed?"}`)
	if httpRecorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d: %s", httpRecorder.Code, http.StatusOK, httpRecorder.Body.String())
	}
	var httpResponse askResponse
	if err := json.Unmarshal(httpRecorder.Body.Bytes(), &httpResponse); err != nil {
		t.Fatalf("decode HTTP response: %v", err)
	}
	assertAskInventoryPublication(t, httpResponse)

	sseRecorder := postAskSSE(
		&AskHandler{Asker: &fakeStreamingAsker{answer: answer}},
		`{"question":"How many repositories are currently indexed?"}`,
	)
	if sseRecorder.Code != http.StatusOK {
		t.Fatalf("SSE status = %d, want %d: %s", sseRecorder.Code, http.StatusOK, sseRecorder.Body.String())
	}
	var sseResponse askResponse
	for _, event := range parseSSEEvents(sseRecorder.Body.String()) {
		if event.event == "answer" {
			if err := json.Unmarshal([]byte(event.data), &sseResponse); err != nil {
				t.Fatalf("decode SSE answer event: %v", err)
			}
		}
	}
	assertAskInventoryPublication(t, sseResponse)
}

func multiPacketExactIndexedRepositoriesAnswer(total int64) AskAnswer {
	primaryIndex := 1
	return AskAnswer{
		Prose:              "provider supplied an unrelated result",
		PrimaryPacketIndex: &primaryIndex,
		Packets: []AnswerPacket{
			{
				PrimaryTool:     "list_collectors",
				TruthClass:      AnswerTruthDerived,
				Summary:         "unrelated supported collector result",
				Supported:       true,
				ResultRef:       "eshu://api-result/collectors",
				Result:          map[string]any{"count": int64(12)},
				CitationRef:     "eshu://citations/unrelated",
				EvidenceHandles: []evidenceCitationHandle{{Kind: "entity", EntityID: "unrelated"}},
			},
			{
				PrimaryTool:     indexedRepositoryInventoryToolForTest,
				TruthClass:      AnswerTruthDeterministic,
				Summary:         "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total.",
				Supported:       true,
				ResultRef:       indexedRepositoriesResultRef,
				Result:          map[string]any{"total": total},
				CitationRef:     "eshu://citations/repository-inventory",
				EvidenceHandles: []evidenceCitationHandle{{Kind: "repository", RepoID: "repository-inventory"}},
			},
		},
		Trace: []AskTraceEntry{
			{Tool: "list_collectors", Supported: true, TruthClass: AnswerTruthDerived},
			{Tool: indexedRepositoryInventoryToolForTest, Supported: true, TruthClass: AnswerTruthDeterministic},
		},
	}
}

func assertAskInventoryPublication(t *testing.T, response askResponse) {
	t.Helper()
	if got, want := response.AnswerProse, "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total."; got != want {
		t.Fatalf("answer_prose = %q, want %q", got, want)
	}
	if got, want := response.TruthClass, string(AnswerTruthDeterministic); got != want {
		t.Fatalf("truth_class = %q, want %q", got, want)
	}
	if got, want := response.ResultRef, indexedRepositoriesResultRef; got != want {
		t.Fatalf("result_ref = %q, want %q", got, want)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", response.Result)
	}
	if got, want := IntVal(result, "total"), 896; got != want {
		t.Fatalf("result total = %d, want %d", got, want)
	}
	if got, want := response.CitationRef, "eshu://citations/repository-inventory"; got != want {
		t.Fatalf("citation_ref = %q, want %q", got, want)
	}
	if got, want := len(response.EvidenceHandles), 1; got != want {
		t.Fatalf("len(evidence_handles) = %d, want %d", got, want)
	}
	if got, want := response.EvidenceHandles[0].RepoID, "repository-inventory"; got != want {
		t.Fatalf("evidence repo_id = %q, want %q", got, want)
	}
	if got, want := len(response.QueryTrace), 2; got != want {
		t.Fatalf("len(query_trace) = %d, want %d", got, want)
	}
	if got, want := response.QueryTrace[0].Tool, "list_collectors"; got != want {
		t.Fatalf("first query trace tool = %q, want %q", got, want)
	}
	if got, want := response.QueryTrace[1].Tool, indexedRepositoryInventoryToolForTest; got != want {
		t.Fatalf("second query trace tool = %q, want %q", got, want)
	}
}

func assertAskAggregateResult(t *testing.T, response askResponse, wantTotal int64) {
	t.Helper()
	if got := response.ResultRef; got != indexedRepositoriesResultRef {
		t.Fatalf("result_ref = %q, want %q", got, indexedRepositoriesResultRef)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", response.Result)
	}
	if got := IntVal(result, "total"); got != int(wantTotal) {
		t.Fatalf("result total = %d, want %d", got, wantTotal)
	}
	if got := response.TruthClass; got != string(AnswerTruthDeterministic) {
		t.Fatalf("truth_class = %q, want %q", got, AnswerTruthDeterministic)
	}
	if len(response.QueryTrace) != 1 || response.QueryTrace[0].Tool != indexedRepositoryInventoryToolForTest || !response.QueryTrace[0].Supported {
		t.Fatalf("query_trace = %#v, want supported %s", response.QueryTrace, indexedRepositoryInventoryToolForTest)
	}
}
