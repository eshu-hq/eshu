// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsSemanticEvidence documents the sanitized semantic evidence fact
// row and its list envelopes. It is included into components' "schemas"
// object as a separate constant fragment to keep components.go under the
// repository's file-size limit, like componentsEntitySearch.
const componentsSemanticEvidence = `      "SemanticEvidenceRow": {
        "type": "object",
        "description": "Sanitized semantic evidence fact row. Prompt payloads, credentials, and private provider responses are not exposed.",
        "properties": {
          "fact_id": {"type": "string", "description": "Durable fact identifier emitted by query.semanticEvidencePublicRow from the fact record."},
          "fact_kind": {"type": "string", "enum": ["semantic.documentation_observation", "semantic.code_hint"], "description": "Durable fact kind emitted by query.semanticEvidencePublicRow: a documentation observation or a code hint."},
          "truth_basis": {"type": "string", "enum": ["semantic_observation", "code_hint"], "description": "Truth basis derived by query.semanticEvidenceTruthBasis from fact_kind."},
          "scope_id": {"type": "string", "description": "Ingestion scope the fact belongs to, copied by query.semanticEvidencePublicRow from the fact record; present when the record carries one."},
          "generation_id": {"type": "string", "description": "Generation the fact was observed at, copied by query.semanticEvidencePublicRow from the fact record; present when the record carries one."},
          "source_system": {"type": "string", "description": "Source system that produced the fact, copied by query.semanticEvidencePublicRow from the fact record; present when the record carries one."},
          "observed_at": {"type": "string", "description": "When the fact was observed, copied by query.semanticEvidencePublicRow from the fact record and the payload; present when either carries it."},
          "observation_id": {"type": "string", "description": "Stable observation identity from the documentation-observation payload, projected by query.semanticEvidencePublicRow; present on observation rows."},
          "observation_type": {"type": "string", "description": "Observation family from the documentation-observation payload, projected by query.semanticEvidencePublicRow; present on observation rows."},
          "observation_text": {"type": "string", "description": "Redacted observation content from the documentation-observation payload, projected by query.semanticEvidencePublicRow; present on observation rows and withheld under a denied or partial access disposition."},
          "observation_hash": {"type": "string", "description": "Content hash of the observation from the documentation-observation payload, projected by query.semanticEvidencePublicRow; present on observation rows."},
          "hint_id": {"type": "string", "description": "Stable hint identity from the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows."},
          "hint_type": {"type": "string", "description": "Hint family from the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows."},
          "relationship_kind": {"type": "string", "description": "Non-canonical relationship kind from the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows that name one."},
          "hint_text": {"type": "string", "description": "Redacted hint content from the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows and withheld under a denied or partial access disposition."},
          "hint_hash": {"type": "string", "description": "Content hash of the hint from the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows."},
          "confidence": {"type": "string", "description": "Provider-reported confidence string carried on the payload, projected by query.semanticEvidencePublicRow; present when the payload carries one. Presentation only, never truth."},
          "unsupported_reason": {"type": "string", "description": "Why the observation or hint is unsupported, carried on the payload and projected by query.semanticEvidencePublicRow; present when set."},
          "provider_profile_id": {"type": "string", "description": "Provider profile that produced the fact, projected by query.semanticEvidencePublicRow from the payload provider block."},
          "provider_kind": {"type": "string", "description": "Provider family that produced the fact, projected by query.semanticEvidencePublicRow from the payload provider block."},
          "model_id": {"type": "string", "description": "Model that produced the fact, flattened by query.semanticEvidencePublicRow from the payload provider block; present when the payload carries one."},
          "endpoint_profile_id": {"type": "string", "description": "Endpoint profile the fact was produced through, flattened by query.semanticEvidencePublicRow from the payload provider block; present when the payload carries one."},
          "prompt_version": {"type": "string", "description": "Prompt version recorded in the payload chunk block, projected by query.semanticEvidencePublicRow."},
          "redaction_version": {"type": "string", "description": "Redaction version recorded in the payload chunk block, projected by query.semanticEvidencePublicRow."},
          "extractor_version": {"type": "string", "description": "Extractor version recorded in the payload chunk block, flattened by query.semanticEvidencePublicRow; present when the payload carries one."},
          "extraction_mode": {"type": "string", "description": "Extraction mode recorded in the payload chunk block, flattened by query.semanticEvidencePublicRow; present when the payload carries one."},
          "promotion_policy": {"type": "string", "description": "Promotion policy carried on the code-hint payload, projected by query.semanticEvidencePublicRow; present on code-hint rows."},
          "redaction_state": {"type": "string", "description": "Bounded redaction state carried on the payload, projected by query.semanticEvidencePublicRow; present when the payload carries one."},
          "redaction_summary": {"type": "string", "description": "Human-readable redaction summary carried on the payload, projected by query.semanticEvidencePublicRow; present when the payload carries one."},
          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Attribution layers the payload reports as missing, projected verbatim by query.semanticEvidencePublicRow from facts/semantic SemanticDocumentationObservationPayload and SemanticCodeHintPayload; present when the payload names any."},
          "evidence_refs": {"type": "array", "description": "Evidence references used by the payload, filtered by query.semanticEvidencePublicRow over the docs.EvidenceRef allowlist; present when the payload carries any.", "items": {"type": "object", "description": "One evidence reference from the payload, filtered to kind, id, uri, and confidence. Owner: query.semanticEvidencePublicRow over the docs.EvidenceRef allowlist.", "properties": {
            "kind": {"type": "string", "description": "Evidence reference kind. Owner: docs.EvidenceRef.Kind."},
            "id": {"type": "string", "description": "Evidence reference identity. Owner: docs.EvidenceRef.ID."},
            "uri": {"type": "string", "description": "Evidence locator URI. Owner: docs.EvidenceRef.URI; present when the payload carries one."},
            "confidence": {"type": "string", "description": "Reference confidence. Owner: docs.EvidenceRef.Confidence; present when the payload carries one."}
          }}},
          "object_refs": {"type": "array", "description": "Code entities the hint references, filtered by query.semanticEvidencePublicRow over the facts/semantic SemanticCodeEntityRef allowlist; present on code-hint rows that reference objects.", "items": {"type": "object", "description": "One referenced code entity from the payload, filtered to repository_id, relative_path, entity_kind, entity_id, line_start, and line_end. Owner: query.semanticEvidencePublicRow over the facts/semantic SemanticCodeEntityRef allowlist.", "properties": {
            "repository_id": {"type": "string", "description": "Referenced entity repository. Owner: facts/semantic SemanticCodeEntityRef.RepositoryID."},
            "relative_path": {"type": "string", "description": "Referenced entity file path. Owner: facts/semantic SemanticCodeEntityRef.RelativePath; present when the payload carries one."},
            "entity_kind": {"type": "string", "description": "Referenced entity kind. Owner: facts/semantic SemanticCodeEntityRef.EntityKind."},
            "entity_id": {"type": "string", "description": "Referenced entity identity. Owner: facts/semantic SemanticCodeEntityRef.EntityID."},
            "line_start": {"type": "integer", "description": "Referenced entity start line. Owner: facts/semantic SemanticCodeEntityRef.LineStart; present when the payload carries one."},
            "line_end": {"type": "integer", "description": "Referenced entity end line. Owner: facts/semantic SemanticCodeEntityRef.LineEnd; present when the payload carries one."}
          }}},
          "policy_state": {"type": "string", "description": "Bounded policy state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "freshness_state": {"type": "string", "description": "Bounded freshness state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "admission_state": {"type": "string", "description": "Bounded admission state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "corroboration_state": {"type": "string", "description": "Bounded corroboration state carried on the payload, projected by query.semanticEvidencePublicRow."},
          "source_acl_state": {"type": "string", "enum": ["allowed", "denied", "partial", "missing", "stale"], "description": "Bounded source-ACL-state observation from the collector, surfaced as a distinct access-posture axis kept separate from freshness_state and policy_state (#2138). Omitted when the source asserted no bounded ACL claim."},
          "access_disposition": {"type": "string", "enum": ["visible", "access_denied", "partial", "stale", "missing"], "description": "Bounded access disposition enforced from source_acl_state and the per-caller read decision (#2164). visible: content intact. access_denied: caller authenticated-but-not-authorized; content withheld and permission_denied set. partial: content withheld behind a partial marker. stale: permitted-but-stale; content intact. missing: source not found at origin. A distinct axis from freshness_state/policy_state; the row is disclosed (not silently dropped) so a reader can tell 'no evidence' from 'evidence exists but is denied/partial/stale'."},
          "permission_denied": {"type": "boolean", "description": "Set true when the caller is authenticated-but-not-authorized for the source (access_denied disposition). The protected content is withheld; only bounded state is disclosed."},
          "content_withheld": {"type": "boolean", "description": "Set true when protected content/excerpt was stripped from the row because the access posture (access_denied or partial) is not cleanly readable. Only bounded identity/state fields remain."},
          "source": {"type": "object", "description": "Filtered source locator projected by query.semanticEvidencePublicRow over the facts/semantic SemanticSourceRef allowlist.", "properties": {
            "source_id": {"type": "string", "description": "Source identity. Owner: facts/semantic SemanticSourceRef.SourceID."},
            "source_class": {"type": "string", "description": "Source class. Owner: facts/semantic SemanticSourceRef.SourceClass."},
            "source_handle": {"type": "string", "description": "Opaque source handle. Owner: facts/semantic SemanticSourceRef.SourceHandle; present when the payload carries one."},
            "repository_id": {"type": "string", "description": "Source repository. Owner: facts/semantic SemanticSourceRef.RepositoryID; present when the payload carries one."},
            "document_id": {"type": "string", "description": "Source document identity. Owner: facts/semantic SemanticSourceRef.DocumentID; present when the payload carries one."},
            "relative_path": {"type": "string", "description": "Source file path. Owner: facts/semantic SemanticSourceRef.RelativePath; present when the payload carries one."},
            "external_anchor": {"type": "string", "description": "External anchor for non-repo sources. Owner: facts/semantic SemanticSourceRef.ExternalAnchor; present when the payload carries one."},
            "section_id": {"type": "string", "description": "Source section identity. Owner: facts/semantic SemanticSourceRef.SectionID; present when the payload carries one."},
            "line_start": {"type": "integer", "description": "Source start line. Owner: facts/semantic SemanticSourceRef.LineStart; present when the payload carries one."},
            "line_end": {"type": "integer", "description": "Source end line. Owner: facts/semantic SemanticSourceRef.LineEnd; present when the payload carries one."},
            "page_start": {"type": "integer", "description": "Source start page. Owner: facts/semantic SemanticSourceRef.PageStart; present when the payload carries one."},
            "page_end": {"type": "integer", "description": "Source end page. Owner: facts/semantic SemanticSourceRef.PageEnd; present when the payload carries one."}
          }},
          "chunk": {"type": "object", "description": "Filtered chunk identity projected by query.semanticEvidencePublicRow over the facts/semantic SemanticChunkRef allowlist.", "properties": {
            "chunk_id": {"type": "string", "description": "Chunk identity. Owner: facts/semantic SemanticChunkRef.ChunkID."},
            "chunk_hash": {"type": "string", "description": "Chunk content hash. Owner: facts/semantic SemanticChunkRef.ChunkHash."},
            "source_hash": {"type": "string", "description": "Source content hash. Owner: facts/semantic SemanticChunkRef.SourceHash."},
            "prompt_version": {"type": "string", "description": "Prompt version. Owner: facts/semantic SemanticChunkRef.PromptVersion."},
            "redaction_version": {"type": "string", "description": "Redaction version. Owner: facts/semantic SemanticChunkRef.RedactionVersion."},
            "extractor_version": {"type": "string", "description": "Extractor version. Owner: facts/semantic SemanticChunkRef.ExtractorVersion."},
            "extraction_mode": {"type": "string", "description": "Extraction mode. Owner: facts/semantic SemanticChunkRef.ExtractionMode."}
          }},
          "provider": {"type": "object", "description": "Filtered provider identity projected by query.semanticEvidencePublicRow over the facts/semantic SemanticProviderRef allowlist.", "properties": {
            "provider_profile_id": {"type": "string", "description": "Provider profile. Owner: facts/semantic SemanticProviderRef.ProviderProfileID."},
            "provider_kind": {"type": "string", "description": "Provider family. Owner: facts/semantic SemanticProviderRef.ProviderKind."},
            "model_id": {"type": "string", "description": "Model identity. Owner: facts/semantic SemanticProviderRef.ModelID; present when the payload carries one."},
            "endpoint_profile_id": {"type": "string", "description": "Endpoint profile. Owner: facts/semantic SemanticProviderRef.EndpointProfileID; present when the payload carries one."}
          }},
          "subject": {"type": "object", "description": "Code entity the hint is about, filtered by query.semanticEvidencePublicRow over the facts/semantic SemanticCodeEntityRef allowlist; present on code-hint rows.", "properties": {
            "repository_id": {"type": "string", "description": "Subject entity repository. Owner: facts/semantic SemanticCodeEntityRef.RepositoryID."},
            "relative_path": {"type": "string", "description": "Subject entity file path. Owner: facts/semantic SemanticCodeEntityRef.RelativePath; present when the payload carries one."},
            "entity_kind": {"type": "string", "description": "Subject entity kind. Owner: facts/semantic SemanticCodeEntityRef.EntityKind."},
            "entity_id": {"type": "string", "description": "Subject entity identity. Owner: facts/semantic SemanticCodeEntityRef.EntityID."},
            "line_start": {"type": "integer", "description": "Subject entity start line. Owner: facts/semantic SemanticCodeEntityRef.LineStart; present when the payload carries one."},
            "line_end": {"type": "integer", "description": "Subject entity end line. Owner: facts/semantic SemanticCodeEntityRef.LineEnd; present when the payload carries one."}
          }}
        }
      },
      "SemanticObservationList": {
        "type": "object",
        "description": "Semantic documentation observation page. Owner: query.semanticEvidenceResponse.",
        "properties": {
          "observations": {"type": "array", "description": "Observation rows in the page. Owner: query.semanticEvidenceResponse.", "items": {"$ref": "#/components/schemas/SemanticEvidenceRow"}},
          "count": {"type": "integer", "description": "Rows returned in this page. Owner: query.semanticEvidenceResponse."},
          "limit": {"type": "integer", "description": "Page bound the rows were cut to. Owner: query.semanticEvidenceResponse."},
          "truncated": {"type": "boolean", "description": "True when more rows exist past this page. Owner: query.semanticEvidenceResponse."},
          "states": {"type": "array", "items": {"type": "string"}, "description": "An empty page reports no_semantic_evidence and, when proven, scope_not_found or no_active_generation."},
          "generation_binding": {"type": "object", "description": "Generation the page was read at. Owner: query.documentationGenerationBindingResponse.", "properties": {"mode": {"type": "string", "enum": ["active", "explicit"], "description": "Whether the read used the active generation or an explicit one. Owner: query.documentationGenerationBindingResponse."}, "generation_id": {"type": "string", "description": "Explicit generation id; empty on the active path. Owner: query.documentationGenerationBindingResponse."}, "is_active": {"type": "boolean", "description": "Whether the bound generation is the active one. Owner: query.documentationGenerationBindingResponse."}}, "required": ["mode", "generation_id", "is_active"]},
          "next_cursor": {"type": "string", "description": "Opaque cursor for the next page; absent when the page is complete. Owner: query.semanticEvidenceResponse."}
        }
      },
      "SemanticCodeHintList": {
        "type": "object",
        "description": "Semantic code hint page. Owner: query.semanticEvidenceResponse.",
        "properties": {
          "code_hints": {"type": "array", "description": "Code hint rows in the page. Owner: query.semanticEvidenceResponse.", "items": {"$ref": "#/components/schemas/SemanticEvidenceRow"}},
          "count": {"type": "integer", "description": "Rows returned in this page. Owner: query.semanticEvidenceResponse."},
          "limit": {"type": "integer", "description": "Page bound the rows were cut to. Owner: query.semanticEvidenceResponse."},
          "truncated": {"type": "boolean", "description": "True when more rows exist past this page. Owner: query.semanticEvidenceResponse."},
          "states": {"type": "array", "items": {"type": "string"}, "description": "An empty page reports no_semantic_evidence and, when proven, scope_not_found or no_active_generation."},
          "generation_binding": {"type": "object", "description": "Generation the page was read at. Owner: query.documentationGenerationBindingResponse.", "properties": {"mode": {"type": "string", "enum": ["active", "explicit"], "description": "Whether the read used the active generation or an explicit one. Owner: query.documentationGenerationBindingResponse."}, "generation_id": {"type": "string", "description": "Explicit generation id; empty on the active path. Owner: query.documentationGenerationBindingResponse."}, "is_active": {"type": "boolean", "description": "Whether the bound generation is active. Owner: query.documentationGenerationBindingResponse."}}, "required": ["mode", "generation_id", "is_active"]},
          "next_cursor": {"type": "string", "description": "Opaque cursor for the next page; absent when the page is complete. Owner: query.semanticEvidenceResponse."}
        }
      },
`
