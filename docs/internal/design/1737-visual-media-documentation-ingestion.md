# Visual Media Documentation Ingestion Design

Status: **PROPOSED - SECURITY REVIEW AND IMPLEMENTATION PROOF REQUIRED.**

Issues: #1737, #1743, #1744, #1745. Parent: #1733. Related baseline:
#1734 / PR #1749 Git-hosted Markdown documentation fact ingestion and
`docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`.

Refs: #1737, #1743, #1744, #1745.

## 1. Decision

Eshu may ingest visual and time-based documentation artifacts only through a
bounded, source-fact-only extraction boundary. The proposed first families are:

- PDF text extraction for `.pdf`.
- Deterministic diagram extraction for `.svg`, `.drawio`, `.excalidraw`,
  `.puml`, `.plantuml`, `.mmd`, `.mermaid`, and `.d2`.
- Local OCR for `.png`, `.jpg`, `.jpeg`, `.webp`, and `.gif`.
- Local transcript extraction for `.mp3`, `.wav`, `.m4a`, `.ogg`, `.mp4`,
  `.mov`, `.webm`, and `.mkv`.

Every extractor emits source-neutral documentation facts only. Extracted text,
diagram labels, OCR output, and transcripts prove what a source artifact
contained at a revision. They do not prove service ownership, deployment state,
architecture truth, incident root cause, or security posture. Reducers and
query surfaces own later comparison, admission, drift finding, and truth labels.

## 2. Non-Goals

This design does not approve:

- production ingestion code
- default-on repository, hosted, or chart/runtime knobs
- canonical graph projection from PDF, diagram, OCR, or transcript facts
- execution of embedded PDF JavaScript, SVG script, diagram includes, PlantUML
  preprocessors, external image references, codecs fetched from the network, or
  active document content
- cloud OCR, cloud transcription, hosted vision models, or LLM summarization
- semantic interpretation of visual layout as operational truth
- storage or public retrieval of raw source bytes, rendered pages, image
  pixels, audio samples, video frames, or model intermediates
- ingestion of private meeting content without source ACL proof and explicit
  source opt-in

## 3. Source Truth Boundary

The extraction path remains:

```text
source artifact -> bounded extractor -> documentation facts -> fact store
  -> reducer/query comparison -> API/MCP bounded reads
```

Collectors, converters, OCR workers, and transcription workers observe source
artifacts and emit:

- `documentation_source`
- `documentation_document`
- `documentation_section`
- `documentation_link`
- optional `documentation_entity_mention`
- optional `documentation_claim_candidate`

Documentation claim candidates use `authority=document_evidence`. They preserve
document evidence only. Ambiguous OCR, diagram, and transcript matches may emit
mentions with `resolution_status=ambiguous` or `unmatched`; they must not emit
exact claim candidates.

## 4. Fact Mapping

| Fact | Mapping rule |
| --- | --- |
| `documentation_source` | One source system boundary such as Git attachment path, Confluence attachment, issue attachment, or configured documentation media source. It carries source system, external ID, source type, owner refs, ACL summary, and source-level metadata only. |
| `documentation_document` | One artifact revision. IDs include source ID, external artifact ID, revision ID, format family, canonical URI, content hash, parent document when extracted from a container, ACL summary, and safe source metadata. |
| `documentation_section` | One bounded page, diagram unit, OCR region group, or transcript time chunk. Sections carry ordinal path, source anchor, source start/end refs, bounded content, text/excerpt hashes, truncation metadata, confidence bucket, and warning flags. |
| `documentation_link` | One explicit link observed in PDF annotations, diagram link attributes, SVG safe links, OCR-detected URLs, or transcript text. Target URI is persisted only after scheme allowlist and sensitive-value redaction. Anchor text persists as a hash unless ACL allows excerpt return. |
| `documentation_entity_mention` | One deterministic mention from extracted section text. Mentions preserve source provenance and may be exact, ambiguous, or unmatched. OCR and transcript mentions include confidence metadata and never force exact resolution. |
| `documentation_claim_candidate` | One non-authoritative claim from deterministic claim-hint rules. It requires document, revision, section, excerpt hash, subject mention, authority `document_evidence`, and confidence/provenance metadata. It must not be emitted from ambiguous subject mentions. |

## 5. Supported And Unsupported Formats

| Family | Supported first slice | Unsupported first slice |
| --- | --- | --- |
| PDF | Text-bearing `.pdf` with page text, safe metadata, page anchors, links, and parse warnings | Encrypted PDFs without supplied credentials, scanned/image-only PDF OCR, embedded files, JavaScript, forms, signatures, annotations beyond counts, malformed xref/object streams beyond repair budget |
| Diagram | `.drawio` XML, `.excalidraw` JSON, `.mmd`, `.mermaid`, `.puml`, `.plantuml`, `.d2`, and safe `.svg` text/links | Remote includes, local includes, external entity expansion, SVG script/event handlers, embedded raster OCR, arbitrary renderer execution, semantic layout inference |
| Image | Static `.png`, `.jpg`, `.jpeg`, `.webp`, first-frame `.gif` OCR and safe metadata | Vision summaries, face/person identification, EXIF location retention, animated GIF all-frame OCR by default, corrupted images, unsupported color profiles, huge dimensions |
| Audio/video | Local transcript extraction for configured short `.mp3`, `.wav`, `.m4a`, `.ogg`, `.mp4`, `.mov`, `.webm`, and `.mkv` | Remote transcription, diarization as identity truth, unsupported codecs, DRM/encrypted media, subtitles as trusted transcript without provenance, long media beyond limit |

Scanned PDF OCR is intentionally handled by the image/OCR issue, not the first
PDF text extractor. Video OCR of frames is also out of scope; video contributes
audio transcript facts only in the first media slice.

## 6. Resource Limits

The first implementation must hard-code these defaults and make runtime config
able to lower them. Raising a limit requires a follow-up design update,
security review, and no-regression proof on the affected collector path.

| Limit | PDF | Diagram | Image/OCR | Audio/video transcript |
| --- | ---: | ---: | ---: | ---: |
| Source file bytes | 50 MiB | 10 MiB | 25 MiB | 250 MiB |
| Expanded or decoded bytes | 128 MiB | 64 MiB | 128 MiB | 512 MiB scratch |
| Pages, elements, frames, or duration | 500 pages | 10,000 nodes/edges/text items | 50 megapixels / 1 GIF frame | 30 minutes |
| Per-artifact wall time | 30s | 15s | 30s | 10 minutes |
| XML/JSON nesting depth | 64 | 64 | not applicable | container metadata only |
| Stored text excerpt | 16 KiB per page or section | 16 KiB per diagram section | 16 KiB per OCR region group | 16 KiB per transcript chunk |
| Section count | 1,000 | 1,000 | 500 OCR regions | 1,000 chunks |
| External network access | disabled | disabled | disabled | disabled |

Extraction must count bytes while streaming. A preflight estimate is not
enough. The reader must stop as soon as cumulative expanded bytes, page count,
element count, pixel budget, media duration, nesting depth, timeout, or scratch
storage budget is exceeded. Temporary files must live in a private per-run
directory, must not be executable, and must be deleted on success, failure, and
context cancellation.

## 7. Format Mapping

### 7.1 PDF Text

PDF ingestion emits one `documentation_document` per source revision. It emits
one `documentation_section` per page, plus optional child sections only when the
extractor deterministically identifies headings or table regions without
rendering or OCR. Page sections use source references such as `page:12` and
anchors such as `pdf-page-12`.

Safe links become `documentation_link` facts with target URI, source page, and
anchor text hash. Tables may be represented as section metadata with row,
column, truncation, and confidence metadata. Embedded files, JavaScript,
annotation text, form field values, and signatures are recorded only as counts
and warning classes until separately reviewed.

The first text extractor must use an isolated helper boundary rather than
linking a PDF text parser into the long-running ingester or hosted collector.
The reviewed candidate is Poppler `pdftotext` because its documented contract is
plain-text conversion from a PDF input to a text output, including stdout
output. Eshu must wrap it in a per-file helper process with network access
disabled, a private non-executable temp directory, context cancellation,
source-byte and output-byte limits, a page range cap, wall-time and memory
limits supplied by the runtime sandbox, and cleanup on every exit path.

Go-native PDF libraries may be used only for preflight, validation, page-count,
encryption, metadata, or attachment detection until a follow-up benchmark and
security review proves page-text extraction quality, cancellation behavior,
malformed-input handling, and memory bounds. `pdfcpu` is the preferred Go
candidate for that validation lane because its public API and CLI support PDF
validation, encryption handling, metadata/content extraction surfaces, and
context-aware reads. The already landed `pdfpreflight` package remains the
default-off first guard and uses only bounded standard-library marker scans.

### 7.2 Diagrams

Diagram ingestion emits one `documentation_document` per diagram revision and
sections for deterministic diagram units:

- Mermaid, PlantUML, and D2: one section per parsed block or diagram file.
- Draw.io: one section per page and optional child sections for cells with
  stable IDs.
- Excalidraw: one section per scene and optional child sections for labeled
  elements.
- SVG: one section for safe document-level text plus optional child sections for
  text elements with stable source anchors.

Diagram node and edge labels may become mentions or claim candidates only when
the text matches an existing deterministic documentation extractor rule.
Diagram edges are document evidence, not Eshu operational graph edges. Any
semantic interpretation of layout, arrows, colors, grouping, or swimlanes must
remain a later low-confidence feature with its own issue and security review.

### 7.3 Image And Screenshot OCR

Image ingestion emits one `documentation_document` per image revision and
bounded OCR region groups as `documentation_section` facts. Region metadata may
include normalized bounding boxes, OCR engine name/version, language hints,
confidence buckets, and redaction warnings. Raw pixels, thumbnails, EXIF GPS
coordinates, camera serials, usernames, and local paths must not be persisted.

OCR text is lower-confidence document evidence. It can produce mentions when a
bounded deterministic rule matches, but claim candidates require exact subject
mentions and explicit confidence metadata. Secret-looking or personal-data-like
regions must be redacted from persisted content and represented by warning
classes and hashes only.

### 7.4 Audio And Video Transcripts

Media ingestion emits one `documentation_document` per audio or video revision.
Transcript chunks become `documentation_section` facts with source references
such as `time:00:05:12.300-00:05:27.800`. When available, speaker labels are
opaque segment labels from the transcription engine, not person identity facts.

Transcript text is document evidence only. Spoken statements may become claim
candidates only through the same deterministic mention and claim-hint gates used
for text documentation. No implementation may treat a transcript assertion as
verified architecture, deployment, ownership, or incident truth without a
separate comparison against source-owned truth.

## 8. Privacy, ACL, And Redaction

All extracted content inherits the source artifact ACL. API/MCP evidence reads
must enforce the same source ACL before returning excerpts, OCR text, diagram
labels, or transcript chunks.

The durable default for sensitive metadata is:

- presence booleans
- bounded counts
- stable fingerprints
- coarse kind or confidence buckets
- source-reported timestamps only when source ACL was evaluated

Raw usernames, emails, ticket IDs, hostnames, tenant IDs, tokens, credentials,
local paths, network shares, customer names, meeting attendees, EXIF location,
and private URLs must not be written to logs, metrics, stable identity maps, or
public docs. If a value is useful for correlation but sensitive, store a keyed
or stable fingerprint and a coarse kind.

ACL handling follows the documentation evidence packet contract:

- source ACL evaluated and allowed: facts may include bounded source-native
  content and authorized reads may return it
- source ACL evaluated and denied: facts may keep non-content metadata, but
  evidence reads deny content
- source ACL missing or partial: set `acl_summary.is_partial`, preserve the
  partial reason, and deny evidence-packet content by default

## 9. Failure Classes

The first implementation must preserve compact failure classes in
`source_metadata` or a follow-up `documentation_warning` fact if per-section
cardinality requires it. Accepted classes:

- `unsupported_format`
- `unsupported_encrypted`
- `unsupported_codec`
- `unsupported_scanned_pdf`
- `unsupported_remote_include`
- `unsupported_active_content`
- `malformed_pdf`
- `malformed_xml`
- `malformed_json`
- `malformed_media`
- `resource_limit_exceeded`
- `timeout`
- `ocr_low_confidence`
- `transcript_no_speech`
- `external_reference_skipped`
- `sensitive_value_redacted`
- `metadata_redacted`
- `annotation_text_skipped`
- `acl_unknown`
- `partial_extraction`

Failures must be visible through fact readback and status evidence. They must
not be swallowed as empty documents.

## 10. Fixture Matrix

| Issue | Required fixtures |
| --- | --- |
| #1737 PDF | text PDF, scanned/image-only PDF, encrypted PDF, malformed PDF, very large PDF, table-heavy PDF, link-heavy PDF, JavaScript-bearing PDF |
| #1743 diagrams | Mermaid, PlantUML, D2, Draw.io XML, Excalidraw JSON, SVG text labels, SVG external reference, SVG script, malformed XML/JSON/diagram text |
| #1744 images | architecture screenshot, dashboard screenshot, secret-looking screenshot, empty image, animated GIF, huge image, corrupt image, EXIF-bearing image |
| #1745 media | short audio, short video, long-duration rejection, corrupt media, unsupported codec, no speech, secret-looking transcript text |

Fixtures must be synthetic and repository-safe. Do not commit private
documents, customer names, private organization names, IP addresses, real
credentials, personal data, proprietary vendor packets, production screenshots,
meeting recordings, or incident audio/video.

## 11. Implementation Sequencing

1. Add failing tests for the shared resource guard, ACL gate, redaction
   classifier, and failure-class serialization before writing extractors.
2. Implement PDF text preflight and page-level extraction first. Prove text PDF,
   malformed, encrypted, oversized, scanned, and table-heavy fixtures before
   emitting durable facts.
3. Implement deterministic diagram parsers with network and include handling
   disabled. Prove diagram labels, links, malformed inputs, SVG active content,
   and external-reference skips before any semantic interpretation is proposed.
4. Implement local OCR as explicit opt-in only. Prove dimension limits,
   confidence buckets, redaction warnings, and first-frame GIF handling.
5. Implement local transcription as explicit opt-in only. Prove duration,
   codec, no-speech, corrupt media, and transcript redaction behavior.
6. Prove source, document, section, link, mention, and claim-candidate fact
   readback through existing documentation API/MCP routes for each family.
7. Add status, logs, spans, and metrics for extraction attempts, result class,
   failure class, elapsed time, bytes read, decoded bytes, page/element/region
   counts, media duration, truncation, and redaction counts. Metric labels must
   stay bounded by source kind, format family, and result class.
8. Run the security review gate. Hosted, repository, and chart/runtime
   enablement stay disabled until security approves resource limits, sandboxing,
   ACL behavior, redaction, dependency posture, and fixture coverage.

## 12. Verification And Security Gates

Every implementation PR under this design must include:

- failing regression tests first for the targeted format family
- package tests for extractor, fact emission, query readback, and MCP parity
- fixture tests for malformed, oversized, unsupported, low-confidence, and
  sensitive cases
- docs updates for any new user-visible format, environment variable,
  capability, route, status field, metric, span, log field, or failure class
- dependency review for parser, OCR, media container, codec, or transcription
  libraries
- sandbox proof for network-disabled execution, temp-file containment, context
  cancellation, and cleanup
- ACL proof that denied or partial sources never return extracted content
- `scripts/test-verify-collector-authoring-gate.sh`
- `scripts/verify-collector-authoring-gate.sh`
- `scripts/test-verify-performance-evidence.sh`
- `scripts/verify-performance-evidence.sh`
- docs build and `git diff --check`

No production collector, chart value, or default runtime path may be added until
the targeted issue receives explicit security approval. These issues use
reference-only issue links until that review is complete.

No-Observability-Change: this note changes design documentation only. Future
runtime work must add or name operator-visible extraction metrics, spans, logs,
and status fields before claiming readiness.

## 13. References

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/facts/documentation.go`
- `go/internal/doctruth/README.md`
- Poppler project: https://poppler.freedesktop.org/
- Poppler `pdftotext` manual:
  https://cgit.freedesktop.org/poppler/poppler/tree/utils/pdftotext.1
- pdfcpu package documentation:
  https://pkg.go.dev/github.com/pdfcpu/pdfcpu/pkg/pdfcpu
