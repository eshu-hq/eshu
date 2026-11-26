// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsEvidencePacket documents the InvestigationEvidencePacket schema:
// the portable investigation_evidence_packet.v2 artifact. Each layer names
// the properties the packet builder emits; free-form layers say so.
// Keep it in lockstep with go/internal/query/querycontract/packet
// (envelope.go layer types, build.go composition and defaults).
const componentsEvidencePacket = `
      "InvestigationEvidencePacket": {
        "type": "object",
        "description": "Portable investigation_evidence_packet.v2 artifact. The packet separates source facts, reducer decisions, graph/query answers, missing evidence, reproduce handles, bounds, redaction, and validation state.",
        "properties": {
          "schema": {"type": "string", "enum": ["investigation_evidence_packet.v2"]},
          "packet_id": {"type": "string", "description": "Deterministic identity derived from the packet identity plus a content digest; the same inputs always produce the same id."},
          "identity": {"type": "object", "description": "Names the investigation a packet describes: its family, the canonical scope keys, the question, the observed generation, and the active profile/backend. Owner: querycontract/packet buildPacketIdentity (envelope.go PacketIdentity).", "required": ["family", "subject", "basis"], "properties": {
            "family": {"type": "string", "enum": ["supply_chain_impact", "deployable_unit", "drift", "service_context"], "description": "Closed investigation family set (envelope.go SupportedInvestigationFamilies). An unrecognized family yields a refusal packet, never a silently empty artifact."},
            "subject": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Canonical scope keys (for example repo_id, workload_id, service_id, advisory_id, package_purl, subject_digest). Values are always strings."},
            "question": {"type": "string", "description": "Canonical normalized question the packet answers."},
            "generation": {"type": "string", "description": "Source/projection generation the evidence was observed at; pins the snapshot so a packet is reproducible against a fixed graph state."},
            "profile": {"type": "string", "enum": ["local_lightweight", "local_authoritative", "local_full_stack", "production"], "description": "Active query profile that produced the evidence."},
            "backend": {"type": "string", "enum": ["neo4j", "nornicdb"], "description": "Active graph backend."},
            "basis": {"type": "string", "enum": ["deterministic", "semantic_augmented"], "description": "deterministic: built only from reducer-owned deterministic evidence. semantic_augmented: additionally carries labeled semantic observations."}
          }},
          "truth": {"type": "object", "nullable": true, "description": "Truth envelope: level, capability, profile, basis, backend, freshness, reason, and omissions. omissions (absent when the response carries every section in full) lists {section, detail, total} for each section a route withheld (detail omitted) or reduced to identity handles (detail handles), such as trace-deployment-chain under evidence_detail handles or an explicit sections list."},
          "freshness": {"type": "object", "description": "Top-level freshness label copied from the truth envelope for quick scanning. Owner: querycontract/packet build.go packetFreshness over querycontract TruthFreshness.", "required": ["state"], "properties": {
            "state": {"type": "string", "enum": ["fresh", "stale", "building", "unavailable"], "description": "Whether the evidence is current and available."},
            "observed_at": {"type": "string", "format": "date-time", "description": "When the freshness state was observed."},
            "detail": {"type": "string", "description": "Human-readable freshness detail."},
            "cause": {"type": "string", "description": "Closed freshness-cause vocabulary naming why the answer is not fresh; empty unless a handler holds evidence for the cause."},
            "next_check": {"type": "object", "description": "Bounded follow-up call that drills into the cause (a status, generation, coverage, citation, or queue surface).", "properties": {
              "tool": {"type": "string", "description": "MCP tool a consumer can call to drill into the cause."},
              "route": {"type": "string", "description": "HTTP route a consumer can call to drill into the cause."},
              "reason": {"type": "string", "description": "Short human-readable explanation of what the next check resolves."},
              "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query parameters for the next check."}
            }}
          }},
          "answer": {"type": "object", "description": "Short user-facing answer plan. No confident summary survives when the answer is unsupported. Owner: querycontract/packet build.go buildPacketAnswer.", "required": ["truth_class", "supported", "partial"], "properties": {
            "truth_class": {"type": "string", "enum": ["deterministic", "derived", "fallback", "semantic_observation", "code_hint", "unsupported"], "description": "Prompt-facing truth classification folded from the truth level and basis axes."},
            "summary": {"type": "string", "description": "Human-readable answer; empty when supported is false."},
            "supported": {"type": "boolean", "description": "False when required evidence is unavailable or the packet was refused."},
            "partial": {"type": "boolean", "description": "True when the answer is usable but incomplete (truncated, stale, or missing hops)."},
            "limitations": {"type": "array", "items": {"type": "string"}, "description": "Bounded human-readable caveats."},
            "unsupported_reasons": {"type": "array", "items": {"type": "string"}, "description": "Explains why the answer is unsupported or partial."}
          }},
          "source_facts": {"type": "array", "description": "Raw-evidence layer: observed facts before any reducer decision. Each entry carries identity and a bounded redaction-safe summary plus an optional addressable citation handle, never the raw payload. Owner: querycontract/packet envelope.go PacketSourceFact.", "items": {"type": "object", "properties": {
            "fact_id": {"type": "string", "description": "Generation-scoped fact identity; optional when stable_key is set."},
            "stable_key": {"type": "string", "description": "Generation-independent durable fact key, preferred for cross-generation references."},
            "evidence_family": {"type": "string", "description": "Fact family (for example vulnerability_advisory, sbom_component, deployment_config)."},
            "collector_kind": {"type": "string", "description": "Collector that observed the fact."},
            "generation": {"type": "string", "description": "Generation the fact was observed at."},
            "subject": {"type": "string", "description": "Canonical entity reference the fact is about."},
            "summary": {"type": "string", "description": "Bounded reducer-approved one-line description."},
            "citation": {"type": "object", "description": "Optional addressable handle into the underlying evidence.", "properties": {
              "kind": {"type": "string"}, "repo_id": {"type": "string"}, "relative_path": {"type": "string"}, "entity_id": {"type": "string"},
              "evidence_family": {"type": "string"}, "reason": {"type": "string"},
              "start_line": {"type": "integer"}, "end_line": {"type": "integer"}, "confidence": {"type": "number"}
            }}
          }}},
          "reducer_decisions": {"type": "array", "description": "Reducer-decision layer: reducer-owned admission, correlation, or drift decisions. Owner: querycontract/packet envelope.go PacketReducerDecision.", "items": {"type": "object", "required": ["state"], "properties": {
            "domain": {"type": "string", "description": "Reducer domain that owns the decision."},
            "subject": {"type": "string", "description": "Canonical entity the decision is about."},
            "state": {"type": "string", "description": "Admission-audit outcome: admitted, rejected, ambiguous, stale, or missing_evidence."},
            "target": {"type": "string", "description": "Edge or node the decision materialized (when admitted)."},
            "reason": {"type": "string", "description": "Bounded human-readable explanation, required for non-admitted states."},
            "generation": {"type": "string", "description": "Generation the decision was made at."},
            "source_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "References into the source-facts layer (by fact_id or stable_key) the decision was based on."}
          }}},
          "graph_answers": {"type": "array", "description": "Graph/query-truth layer: materialized edge or path hops the query surface returns, with prompt-facing truth classes. Owner: querycontract/packet envelope.go PacketGraphAnswer.", "items": {"type": "object", "required": ["present"], "properties": {
            "relationship": {"type": "string", "description": "Edge type (for example RUNS_IN, HANDLES_ROUTE)."},
            "from": {"type": "string", "description": "Canonical source node reference."},
            "to": {"type": "string", "description": "Canonical target node reference."},
            "hop": {"type": "string", "description": "Path-hop label (for example image, workload, service)."},
            "present": {"type": "boolean", "description": "Whether the hop/edge is materialized in the graph."},
            "truth_class": {"type": "string", "enum": ["deterministic", "derived", "fallback", "semantic_observation", "code_hint", "unsupported"], "description": "Prompt-facing classification of the answer truth."},
            "source_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Source-fact ids backing a present hop, so a reader can trace the graph answer to raw evidence."}
          }}},
          "citations": {"type": "array", "description": "Addressable-evidence layer in the evidence-citation handle shape. Owner: querycontract/evidence citation_handle.go EvidenceCitationHandle.", "items": {"type": "object", "properties": {
            "kind": {"type": "string", "description": "Citation kind."},
            "repo_id": {"type": "string", "description": "Repository the cited evidence belongs to."},
            "relative_path": {"type": "string", "description": "Repository-relative file path of the cited evidence."},
            "entity_id": {"type": "string", "description": "Cited entity id, when the citation names an entity."},
            "evidence_family": {"type": "string", "description": "Evidence family of the cited row."},
            "reason": {"type": "string", "description": "Why this handle was cited."},
            "start_line": {"type": "integer", "description": "First cited line."},
            "end_line": {"type": "integer", "description": "Last cited line."},
            "confidence": {"type": "number", "description": "Provider-reported confidence; presentation only, never truth."}
          }}},
          "missing_evidence": {"type": "array", "description": "Explicit missing-hop layer: a gap is named and explained, never silently dropped. Owner: querycontract/packet envelope.go PacketMissingHop.", "items": {"type": "object", "required": ["hop", "reason"], "properties": {
            "hop": {"type": "string", "description": "Path-hop label that could not be resolved."},
            "reason": {"type": "string", "description": "Why the hop is unresolved."},
            "next_check": {"type": "object", "additionalProperties": true, "description": "Free-form optional bounded follow-up call (tool/route/reason shape) that would resolve or explain the gap."}
          }}},
          "semantic_observations": {"type": "array", "description": "Optional policy-gated semantic layer, only permitted under the semantic_augmented basis; never deterministic truth. Owner: querycontract/packet envelope.go PacketSemanticObservation.", "items": {"type": "object", "required": ["label", "observation"], "properties": {
            "label": {"type": "string", "description": "Always semantic_observation so a reader can never mistake the entry for deterministic truth."},
            "provider": {"type": "string", "description": "Semantic provider that produced the observation."},
            "observation": {"type": "string", "description": "Bounded semantic note."},
            "confidence": {"type": "string", "description": "Optional provider-reported confidence label."},
            "source_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Source facts the observation was derived from."}
          }}},
          "reproduce": {"type": "array", "description": "Bounded commands, routes, or tools a reader can run to reproduce the packet evidence. Owner: querycontract/packet envelope.go PacketReproduceStep.", "items": {"type": "object", "properties": {
            "description": {"type": "string", "description": "What the step reproduces."},
            "route": {"type": "string", "description": "HTTP route that produced the evidence."},
            "tool": {"type": "string", "description": "MCP tool that produced the evidence."},
            "command": {"type": "string", "description": "CLI command that produced the evidence."}
          }}},
          "bounds": {"type": "object", "description": "Per-layer caps applied to the packet and whether any layer was capped, so a large investigation produces a bounded artifact that signals truncation. Owner: querycontract/packet envelope.go PacketBounds via applyPacketBounds.", "properties": {
            "max_source_facts": {"type": "integer", "description": "Cap on the source-facts layer (default 200; overrides may only lower it)."},
            "max_reducer_decisions": {"type": "integer", "description": "Cap on the reducer-decision layer (default 200)."},
            "max_graph_answers": {"type": "integer", "description": "Cap on the graph/query-truth layer (default 200)."},
            "max_citations": {"type": "integer", "description": "Cap on the citations layer (defaults to the evidence-citation route cap)."},
            "max_semantic_observations": {"type": "integer", "description": "Cap on the optional semantic layer (default 50)."},
            "max_missing_evidence": {"type": "integer", "description": "Cap on the missing-evidence layer (default 200)."},
            "truncated": {"type": "boolean", "description": "True when any layer was capped."},
            "truncated_layers": {"type": "array", "items": {"type": "string"}, "description": "Layers that were capped, in apply order."}
          }},
          "redaction": {"type": "object", "description": "Share-safe redaction posture: the packet carries only addressable handles and bounded reducer-approved summaries, never raw payloads, secrets, or transport metadata. Owner: querycontract/packet build.go defaultPacketRedaction.", "properties": {
            "profile": {"type": "string", "description": "Redaction profile identifier (share_safe_v2)."},
            "version": {"type": "string", "description": "Redaction profile version (2)."},
            "applied_rules": {"type": "array", "items": {"type": "string"}, "description": "Redaction rules the profile applies (addressable_handles_only, reducer_approved_summaries, no_raw_fact_payloads, no_transport_metadata)."},
            "replaced_fields": {"type": "array", "items": {"type": "string"}, "description": "Fields whose values were replaced by redaction; empty when none were."}
          }},
          "validation": {"type": "object", "description": "Artifact validation outcome. Owner: querycontract/packet validateInvestigationPacket.", "required": ["status", "checks"], "properties": {
            "status": {"type": "string", "enum": ["passed", "failed"], "description": "Overall validation outcome."},
            "checks": {"type": "array", "description": "Every gate result so a reader sees which rule a packet broke.", "items": {"type": "object", "required": ["id", "ok"], "properties": {
              "id": {"type": "string", "description": "Stable gate identifier (for example schema_consistent)."},
              "ok": {"type": "boolean", "description": "Whether the gate passed."}
            }}}
          }},
          "refusal": {"type": "string", "description": "Terminal refusal state for a packet that could not be built into a supported answer (unknown_family, scope_not_found, profile_unsupported, backend_unavailable); empty for a built packet."}
        }
      },
`
