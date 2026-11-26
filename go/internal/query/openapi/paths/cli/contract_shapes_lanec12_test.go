// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC12CliRequests pins the lane-C12 request-shape
// completion for the five CLI-support routes. Every request body the
// handlers strict-decode (decodeBody with DisallowUnknownFields, or the
// strict scoreEvidence shape check) must name the exact keys its owner
// struct accepts instead of a bare {"type": "object"}: a misspelled key
// otherwise fails closed with 400 and the contract gave the caller no way
// to know the accepted shape. Owners: cli/support/handler.go request
// structs (RenderRequest, ComposeRequest, CaptureInput via reportbundle,
// ValidateRequest, answerquality.Evidence).
func TestContractShapesLaneC12CliRequests(t *testing.T) {
	t.Parallel()

	fragment := func(t *testing.T, raw, where string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		var doc map[string]any
		if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
			t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
		}
		return doc
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	requestSchemaAt := func(t *testing.T, doc map[string]any, path, method string) map[string]any {
		t.Helper()
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("fragment paths missing %s", path)
		}
		op := child(t, route, method, path)
		body := child(t, op, "requestBody", path+" "+method)
		content, ok := body["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s requestBody has no content", path, method)
		}
		jsonBody := child(t, content, "application/json", path+" "+method)
		return child(t, jsonBody, "schema", path+" "+method)
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	// requireNoBare walks a schema recursively: every object node must
	// carry properties, additionalProperties, or a $ref/allOf/oneOf
	// composition; every array node must carry items.
	requireNoBare := func(t *testing.T, node map[string]any, where string) {
		t.Helper()
		var walk func(n map[string]any, at string)
		walk = func(n map[string]any, at string) {
			if len(n) == 1 {
				if _, ok := n["$ref"]; ok {
					return
				}
			}
			if n["type"] == "object" {
				_, hasProps := n["properties"]
				_, hasAdditional := n["additionalProperties"]
				_, hasAllOf := n["allOf"]
				_, hasOneOf := n["oneOf"]
				if !hasProps && !hasAdditional && !hasAllOf && !hasOneOf {
					t.Errorf("%s: bare object at %q (no properties, additionalProperties, or composition)", where, at)
				}
			}
			if n["type"] == "array" {
				items, ok := n["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: array at %q has no items", where, at)
				} else {
					walk(items, at+"[]")
				}
			}
			if props, ok := n["properties"].(map[string]any); ok {
				for k, v := range props {
					if vm, ok := v.(map[string]any); ok {
						walk(vm, at+"."+k)
					}
				}
			}
			for _, key := range []string{"allOf", "oneOf"} {
				if branches, ok := n[key].([]any); ok {
					for _, b := range branches {
						if bm, ok := b.(map[string]any); ok {
							walk(bm, at+"/"+key+"[]")
						}
					}
				}
			}
		}
		walk(node, where)
	}

	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := make(map[string]bool, len(keys))
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not accepted by the owner", where, key)
			}
		}
	}

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				if _, ok := node["$ref"]; !ok {
					t.Errorf("%s: property %q has no description", where, key)
				}
			}
		}
	}

	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	// pinTruth pins a querycontract.TruthEnvelope object carried on a
	// request (compose truth inputs): level plus a freshness state always
	// ride; everything else is omitempty. Mirrors the pinned response
	// truth shape (service leaf lane-2 pin).
	pinTruth := func(t *testing.T, node map[string]any, where string) {
		t.Helper()
		props := propsOf(t, node, where)
		requireExactKeys(t, props, where,
			"level", "capability", "profile", "basis", "backend", "freshness",
			"reason", "truncated", "omissions")
		requireScalarTypes(t, props, where, map[string]string{
			"level": "string", "capability": "string", "profile": "string",
			"basis": "string", "backend": "string", "freshness": "object",
			"reason": "string", "truncated": "boolean", "omissions": "array",
		})
		requireDescribed(t, props, where,
			"level", "capability", "profile", "basis", "backend", "freshness",
			"reason", "truncated", "omissions")
		fresh := propsOf(t, child(t, props, "freshness", where), where+".freshness")
		requireExactKeys(t, fresh, where+".freshness",
			"state", "observed_at", "detail", "cause", "next_check")
		requireDescribed(t, fresh, where+".freshness",
			"state", "observed_at", "detail", "cause", "next_check")
		check := propsOf(t, child(t, fresh, "next_check", where), where+".check")
		requireExactKeys(t, check, where+".check",
			"tool", "route", "reason", "params")
		requireDescribed(t, check, where+".check",
			"tool", "route", "reason", "params")
		om := itemsOf(t, child(t, props, "omissions", where), where+".om[]")
		omProps := propsOf(t, om, where+".om[]")
		requireExactKeys(t, omProps, where+".om[]", "section", "detail", "total")
		requireScalarTypes(t, omProps, where+".om[]", map[string]string{"total": "integer"})
		requireDescribed(t, omProps, where+".om[]", "section", "detail", "total")
		requireNoBare(t, node, where)
	}

	t.Run("render request names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go RenderRequest{packet, format}:
		// the packet decodes into packet.InvestigationEvidencePacket
		// (packet_id required by renderPacket) and format parses via
		// packet.ParseInvestigationPacketFormat.
		doc := fragment(t, Render, "Render")
		schema := requestSchemaAt(t, doc, "/api/v0/investigation/packet/render", "post")
		props := propsOf(t, schema, "render request")
		requireExactKeys(t, props, "render request", "packet", "format")
		packet := child(t, props, "packet", "render request.packet")
		if _, ok := packet["description"]; !ok {
			t.Errorf("render request.packet: missing description naming the owner")
		}
		branches, ok := packet["allOf"].([]any)
		if !ok || len(branches) != 1 {
			t.Fatalf("render request.packet: want allOf with one $ref branch")
		}
		branch, ok := branches[0].(map[string]any)
		if !ok {
			t.Fatalf("render request.packet: allOf branch is not an object")
		}
		if ref, _ := branch["$ref"].(string); ref != "#/components/schemas/InvestigationEvidencePacket" {
			t.Errorf("render request.packet: $ref %q, want the pinned packet component", ref)
		}
		requireScalarTypes(t, props, "render request", map[string]string{"format": "string"})
		requireNoBare(t, schema, "render request")
	})

	t.Run("compose request names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go ComposeRequest{dossier, truth,
		// supply_chain, supply_chain_truth}: dossier and supply_chain are
		// map[string]any passthroughs; both truth inputs are
		// *querycontract.TruthEnvelope.
		doc := fragment(t, Compose, "Compose")
		schema := requestSchemaAt(t, doc, "/api/v0/service/report/compose", "post")
		props := propsOf(t, schema, "compose request")
		requireExactKeys(t, props, "compose request",
			"dossier", "truth", "supply_chain", "supply_chain_truth")
		requireDescribed(t, props, "compose request",
			"dossier", "truth", "supply_chain", "supply_chain_truth")
		for _, key := range []string{"dossier", "supply_chain"} {
			node := child(t, props, key, "compose request."+key)
			if node["type"] != "object" {
				t.Errorf("compose request.%s: type %v, want object", key, node["type"])
			}
			if _, ok := node["additionalProperties"]; !ok {
				t.Errorf("compose request.%s: open map input with no additionalProperties", key)
			}
		}
		pinTruth(t, child(t, props, "truth", "compose request.truth"), "compose request.truth")
		pinTruth(t, child(t, props, "supply_chain_truth", "compose request.supply_chain_truth"), "compose request.supply_chain_truth")
		requireNoBare(t, schema, "compose request")
	})

	t.Run("capture request names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: reportbundle.CaptureInput: the surface/target/envelope
		// triple the handler requires, plus the optional capture knobs.
		// The envelope is querycontract.ResponseEnvelope; citations and
		// fact_refs are reportbundle.CitationRef/FactRef rows; payload
		// rows carry CitationExcerpt/facts.Envelope shapes.
		doc := fragment(t, Capture, "Capture")
		schema := requestSchemaAt(t, doc, "/api/v0/report/bundle/capture", "post")
		props := propsOf(t, schema, "capture request")
		requireExactKeys(t, props, "capture request",
			"surface", "target", "method", "params", "profile",
			"reporter_note", "envelope", "truncated", "citations",
			"fact_refs", "fact_refs_reason", "include_payloads",
			"payload_excerpts", "payload_facts")
		requireDescribed(t, props, "capture request", "params", "envelope")
		requireScalarTypes(t, props, "capture request", map[string]string{
			"surface": "string", "target": "string", "method": "string",
			"params": "object", "profile": "string", "reporter_note": "string",
			"envelope": "object", "truncated": "boolean",
			"citations": "array", "fact_refs": "array",
			"fact_refs_reason": "string", "include_payloads": "boolean",
			"payload_excerpts": "array", "payload_facts": "array",
		})
		params := child(t, props, "params", "capture request.params")
		if _, ok := params["additionalProperties"]; !ok {
			t.Errorf("capture request.params: open map input with no additionalProperties")
		}

		// Owner: querycontract.ResponseEnvelope{data, truth, error}.
		envelope := propsOf(t, child(t, props, "envelope", "capture request.envelope"), "capture request.envelope")
		requireExactKeys(t, envelope, "capture request.envelope", "data", "truth", "error")
		requireDescribed(t, envelope, "capture request.envelope", "data", "truth", "error")
		data := child(t, envelope, "data", "capture request.envelope.data")
		if _, ok := data["additionalProperties"]; !ok {
			t.Errorf("capture request.envelope.data: open map input with no additionalProperties")
		}
		pinTruth(t, child(t, envelope, "truth", "capture request.envelope.truth"), "capture request.envelope.truth")

		// Owner: querycontract.ErrorEnvelope: code and message always
		// ride; details is the caller-supplied detail map.
		errNode := propsOf(t, child(t, envelope, "error", "capture request.envelope.error"), "capture request.envelope.error")
		requireExactKeys(t, errNode, "capture request.envelope.error",
			"code", "message", "capability", "correlation_id", "profiles", "details")
		requireDescribed(t, errNode, "capture request.envelope.error",
			"code", "message", "capability", "correlation_id", "profiles", "details")
		details := child(t, errNode, "details", "capture request.envelope.error.details")
		if _, ok := details["additionalProperties"]; !ok {
			t.Errorf("capture request.envelope.error.details: open map input with no additionalProperties")
		}
		profiles := propsOf(t, child(t, errNode, "profiles", "capture request.envelope.error.profiles"), "capture request.envelope.error.profiles")
		requireExactKeys(t, profiles, "capture request.envelope.error.profiles", "current", "required")
		requireDescribed(t, profiles, "capture request.envelope.error.profiles", "current", "required")

		// Owner: reportbundle.CitationRef: every field omitempty, so no
		// required keys; the 12 handle fields plus the citation build.
		cite := propsOf(t, itemsOf(t, child(t, props, "citations", "capture request.citations"), "capture request.citations[]"), "capture request.citations[]")
		requireExactKeys(t, cite, "capture request.citations[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line",
			"confidence", "citation_id", "content_hash", "commit_sha")
		requireDescribed(t, cite, "capture request.citations[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line",
			"confidence", "citation_id", "content_hash", "commit_sha")
		requireScalarTypes(t, cite, "capture request.citations[]", map[string]string{
			"start_line": "integer", "end_line": "integer", "confidence": "number",
		})

		// Owner: reportbundle.FactRef: the durable fact reference; only
		// schema_version is omitempty.
		fref := propsOf(t, itemsOf(t, child(t, props, "fact_refs", "capture request.fact_refs"), "capture request.fact_refs[]"), "capture request.fact_refs[]")
		requireExactKeys(t, fref, "capture request.fact_refs[]",
			"fact_id", "stable_fact_key", "fact_kind", "schema_version",
			"scope_id", "generation_id")
		requireDescribed(t, fref, "capture request.fact_refs[]",
			"fact_id", "stable_fact_key", "fact_kind", "schema_version",
			"scope_id", "generation_id")

		// Owner: reportbundle.CitationExcerpt: the CitationRef handle
		// fields flattened with the raw excerpt bytes.
		excerpt := propsOf(t, itemsOf(t, child(t, props, "payload_excerpts", "capture request.payload_excerpts"), "capture request.payload_excerpts[]"), "capture request.payload_excerpts[]")
		requireExactKeys(t, excerpt, "capture request.payload_excerpts[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line",
			"confidence", "citation_id", "content_hash", "commit_sha",
			"excerpt")
		requireDescribed(t, excerpt, "capture request.payload_excerpts[]", "excerpt")
		requireScalarTypes(t, excerpt, "capture request.payload_excerpts[]", map[string]string{"excerpt": "string"})

		// Owner: facts.Envelope: the durable fact representation; it
		// carries no json tags so the wire keys are the Go field names.
		fact := propsOf(t, itemsOf(t, child(t, props, "payload_facts", "capture request.payload_facts"), "capture request.payload_facts[]"), "capture request.payload_facts[]")
		requireExactKeys(t, fact, "capture request.payload_facts[]",
			"FactID", "ScopeID", "GenerationID", "FactKind",
			"StableFactKey", "SchemaVersion", "CollectorKind",
			"FencingToken", "SourceConfidence", "ObservedAt",
			"Payload", "IsTombstone", "SourceRef")
		requireDescribed(t, fact, "capture request.payload_facts[]",
			"FactID", "ScopeID", "GenerationID", "FactKind",
			"StableFactKey", "SchemaVersion", "CollectorKind",
			"FencingToken", "SourceConfidence", "ObservedAt",
			"Payload", "IsTombstone", "SourceRef")
		requireScalarTypes(t, fact, "capture request.payload_facts[]", map[string]string{
			"FactID": "string", "ScopeID": "string", "GenerationID": "string",
			"FactKind": "string", "StableFactKey": "string",
			"SchemaVersion": "string", "CollectorKind": "string",
			"FencingToken": "integer", "SourceConfidence": "string",
			"ObservedAt": "string", "Payload": "object",
			"IsTombstone": "boolean", "SourceRef": "object",
		})
		payload := child(t, fact, "Payload", "capture request.payload_facts[].Payload")
		if _, ok := payload["additionalProperties"]; !ok {
			t.Errorf("capture request.payload_facts[].Payload: open map input with no additionalProperties")
		}
		ref := propsOf(t, child(t, fact, "SourceRef", "capture request.payload_facts[].SourceRef"), "capture request.payload_facts[].SourceRef")
		requireExactKeys(t, ref, "capture request.payload_facts[].SourceRef",
			"SourceSystem", "ScopeID", "GenerationID", "FactKey",
			"SourceURI", "SourceRecordID")
		requireDescribed(t, ref, "capture request.payload_facts[].SourceRef",
			"SourceSystem", "ScopeID", "GenerationID", "FactKey",
			"SourceURI", "SourceRecordID")

		requireNoBare(t, schema, "capture request")
	})

	t.Run("validate request names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: cli/support/handler.go ValidateRequest{bundle,
		// require_public}: the bundle is the reportbundle.Bundle wire
		// shape (mirrored from the capture 200 contract, which pins the
		// identical type), plus the --require-public posture.
		doc := fragment(t, Validate, "Validate")
		schema := requestSchemaAt(t, doc, "/api/v0/report/bundle/validate", "post")
		props := propsOf(t, schema, "validate request")
		requireExactKeys(t, props, "validate request", "bundle", "require_public")
		requireDescribed(t, props, "validate request", "bundle", "require_public")
		requireScalarTypes(t, props, "validate request", map[string]string{"require_public": "boolean"})
		bundle := propsOf(t, child(t, props, "bundle", "validate request.bundle"), "validate request.bundle")
		requireExactKeys(t, bundle, "validate request.bundle",
			"schema_version", "bundle_id", "created_at", "reporter_note",
			"query", "response", "evidence", "redaction", "payloads",
			"validation")
		requireDescribed(t, bundle, "validate request.bundle",
			"schema_version", "bundle_id", "created_at", "reporter_note",
			"query", "response", "evidence", "redaction", "payloads",
			"validation")
		requireNoBare(t, schema, "validate request")
	})

	t.Run("score request names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: answerquality.Evidence{version, run_id, eshu_commit,
		// prompts}: prompts[] rows are answerquality.PromptResult with
		// per-surface answerquality.SurfaceResult rows; narration drills
		// into answernarration.Input, which stays an open map here
		// because its packet subtree is the full answer-packet contract.
		doc := fragment(t, Score, "Score")
		schema := requestSchemaAt(t, doc, "/api/v0/answer-quality/score", "post")
		props := propsOf(t, schema, "score request")
		requireExactKeys(t, props, "score request",
			"version", "run_id", "eshu_commit", "prompts")
		requireDescribed(t, props, "score request", "version")
		requireScalarTypes(t, props, "score request", map[string]string{
			"version": "string", "run_id": "string", "eshu_commit": "string",
			"prompts": "array",
		})
		prompt := propsOf(t, itemsOf(t, child(t, props, "prompts", "score request.prompts"), "score request.prompts[]"), "score request.prompts[]")
		requireExactKeys(t, prompt, "score request.prompts[]",
			"id", "family", "prompt", "expected_truth_class",
			"required_surfaces", "required_next_calls",
			"acceptable_limitations", "results")
		requireDescribed(t, prompt, "score request.prompts[]",
			"id", "family", "prompt", "expected_truth_class",
			"required_surfaces", "required_next_calls",
			"acceptable_limitations", "results")
		result := propsOf(t, itemsOf(t, child(t, prompt, "results", "score request.prompts[].results"), "score request.prompts[].results[]"), "score request.prompts[].results[]")
		requireExactKeys(t, result, "score request.prompts[].results[]",
			"surface", "useful", "supported", "answer_summary",
			"truth_class", "freshness", "partial", "truncated",
			"citation_handles", "limitations", "next_calls",
			"too_generic", "stale_without_cause", "over_confident",
			"too_verbose", "missing_follow_up", "narration")
		requireDescribed(t, result, "score request.prompts[].results[]",
			"surface", "useful", "supported", "answer_summary",
			"truth_class", "freshness", "partial", "truncated",
			"citation_handles", "limitations", "next_calls",
			"too_generic", "stale_without_cause", "over_confident",
			"too_verbose", "missing_follow_up", "narration")
		requireScalarTypes(t, result, "score request.prompts[].results[]", map[string]string{
			"surface": "string", "useful": "boolean", "supported": "boolean",
			"answer_summary": "string", "truth_class": "string",
			"freshness": "string", "partial": "boolean", "truncated": "boolean",
			"too_generic": "boolean", "stale_without_cause": "boolean",
			"over_confident": "boolean", "too_verbose": "boolean",
			"missing_follow_up": "boolean", "narration": "object",
		})
		narration := child(t, result, "narration", "score request.prompts[].results[].narration")
		if _, ok := narration["additionalProperties"]; !ok {
			t.Errorf("score request.prompts[].results[].narration: bounded map input with no additionalProperties")
		}
		if desc, _ := narration["description"].(string); desc == "" {
			t.Errorf("score request.prompts[].results[].narration: missing description naming the owner")
		}
		requireNoBare(t, schema, "score request")
	})
}
