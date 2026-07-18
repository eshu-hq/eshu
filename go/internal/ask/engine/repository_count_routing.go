// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	indexedRepositoryInventoryTool = "list_indexed_repositories"
	indexedRepositoryResultRef     = "eshu://api-result/repositories"
)

var (
	indexedRepositoryCountCorePatterns = []*regexp.Regexp{
		regexp.MustCompile(`^how many (?:currently )?indexed repositories(?: are there)?$`),
		regexp.MustCompile(`^how many repositories are (?:currently )?indexed$`),
		regexp.MustCompile(`^(?:return|give|show|tell me) (?:only )?(?:the )?(?:exact )?(?:count|number|total)(?: of)? (?:currently )?indexed repositories$`),
		regexp.MustCompile(`^(?:what is|what'?s) (?:the )?(?:exact )?(?:count|number|total) of (?:currently )?indexed repositories$`),
		regexp.MustCompile(`^(?:the )?(?:exact )?(?:count|number|total) of (?:currently )?indexed repositories$`),
		regexp.MustCompile(`^count (?:the )?(?:currently )?indexed repositories$`),
	}
	indexedRepositoryEvidenceSuffix = regexp.MustCompile(
		` (?:and|then) (?:cite|name|include|show) (?:the )?(?:evidence(?: source)?|source)(?: used)?$`,
	)
	indexedRepositorySupportClause = regexp.MustCompile(
		`^(?:(?:and|then) )?(?:(?:return|give|show) (?:only )?(?:the )?(?:exact )?(?:count|number|total)|(?:cite|name|include|show) (?:the )?(?:evidence(?: source)?|source)(?: used)?)$`,
	)
)

// routeIndexedRepositoryCountCalls prevents exact indexed-repository totals
// from being sourced from broader ecosystem or runtime status counters. Those
// counters describe different populations; the repository inventory's total
// field is the canonical count independent of page size.
func routeIndexedRepositoryCountCalls(question string, calls []provider.ToolCall) []provider.ToolCall {
	if !asksForIndexedRepositoryCount(question) || len(calls) == 0 {
		return calls
	}
	routed := append([]provider.ToolCall(nil), calls...)
	for i := range routed {
		switch routed[i].Name {
		case "get_ecosystem_overview", "get_index_status":
			routed[i].Name = indexedRepositoryInventoryTool
			routed[i].Arguments = map[string]any{"limit": 1, "offset": 0}
		case indexedRepositoryInventoryTool:
			// The exact count needs only the authoritative total, not a page of
			// rows, and the total is independent of page size. Bound a bare
			// inventory call to limit=1 so it is never an unscoped list-all: an
			// unbounded inventory read is refused by the pre-dispatch bounding
			// guard and, in production, returns the whole repository inventory
			// and blows the response budget (issue #5266).
			routed[i].Arguments = boundedInventoryCountArgs(routed[i].Arguments)
		}
	}
	return routed
}

// boundedInventoryCountArgs returns the inventory call arguments bounded to a
// single-row page for the exact-count path: it preserves any caller-supplied
// arguments but forces a positive limit and a zero offset when the call is not
// already bounded, so the count read stays within the response budget.
func boundedInventoryCountArgs(args map[string]any) map[string]any {
	if hasPositiveLimit(args) {
		return args
	}
	bounded := make(map[string]any, len(args)+2)
	for k, v := range args {
		bounded[k] = v
	}
	bounded["limit"] = 1
	bounded["offset"] = 0
	return bounded
}

func asksForIndexedRepositoryCount(question string) bool {
	clauses := strings.FieldsFunc(question, func(r rune) bool {
		switch r {
		case '?', '!', '.', ',', ';', ':':
			return true
		default:
			return false
		}
	})
	matchedCore := false
	for _, rawClause := range clauses {
		clause := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(rawClause, "’", "'"))), " ")
		if clause == "" {
			continue
		}
		clause = indexedRepositoryEvidenceSuffix.ReplaceAllString(clause, "")
		if matchesIndexedRepositoryCountCore(clause) {
			if matchedCore {
				return false
			}
			matchedCore = true
			continue
		}
		if indexedRepositorySupportClause.MatchString(clause) {
			continue
		}
		return false
	}
	return matchedCore
}

func matchesIndexedRepositoryCountCore(clause string) bool {
	for _, pattern := range indexedRepositoryCountCorePatterns {
		if pattern.MatchString(clause) {
			return true
		}
	}
	return false
}

func indexedRepositoryCountValue(envelope *query.ResponseEnvelope) (int64, bool) {
	if envelope == nil || envelope.Data == nil {
		return 0, false
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		return 0, false
	}
	var payload struct {
		Count json.Number `json:"count"`
		Total json.Number `json:"total"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return 0, false
	}
	count, err := payload.Count.Int64()
	if err != nil || count < 0 {
		return 0, false
	}
	total, err := payload.Total.Int64()
	if err != nil || total < count {
		return 0, false
	}
	return total, true
}

func indexedRepositoryCountSummary(total int64) string {
	return fmt.Sprintf(
		"%d indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total.",
		total,
	)
}

func applyIndexedRepositoryCountResult(packet *query.AnswerPacket, envelope *query.ResponseEnvelope) {
	packet.Summary = ""
	packet.ResultRef = ""
	packet.Result = nil
	if !packet.Supported || packet.Partial {
		return
	}
	total, ok := indexedRepositoryCountValue(envelope)
	if !ok {
		packet.Partial = true
		packet.UnsupportedReasons = appendLimitation(
			packet.UnsupportedReasons,
			"authoritative indexed repository total is missing or inconsistent with page count",
		)
		return
	}
	packet.Summary = indexedRepositoryCountSummary(total)
	packet.ResultRef = indexedRepositoryResultRef
	packet.Result = map[string]any{"total": total}
}

// finalizeIndexedRepositoryCountAnswer closes the exact-count path over the
// deterministic inventory packet instead of publishing provider prose. This
// guarantees the answer cannot substitute an ecosystem/status counter after
// the authoritative tool call, and fails bounded when the total is absent.
func finalizeIndexedRepositoryCountAnswer(question string, answer *Answer) bool {
	if !asksForIndexedRepositoryCount(question) {
		return false
	}
	for index, packet := range answer.Packets {
		if packet.PrimaryTool != indexedRepositoryInventoryTool {
			continue
		}
		// Bind publication to the inventory packet even when it is unavailable.
		// Otherwise a preceding unrelated supported packet could become the
		// response fallback while this exact-count path correctly fails bounded.
		answer.PrimaryPacketIndex = &index
		if !packet.Supported || packet.Partial {
			continue
		}
		if strings.TrimSpace(packet.Summary) != "" {
			answer.Prose = packet.Summary
			answer.Narrated = false
			return true
		}
	}
	answer.Prose = ""
	answer.Narrated = false
	answer.Partial = true
	answer.Limitations = appendLimitation(
		answer.Limitations,
		"authoritative indexed repository total unavailable",
	)
	return true
}
