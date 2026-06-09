# Office, Spreadsheet, Deck, And Archive Documentation Ingestion Design

Status: **PROPOSED - SECURITY REVIEW AND IMPLEMENTATION PROOF REQUIRED.**

Issues: #1738, #1739, #1740, #1747. Parent: #1733. Related baseline:
#1734 / PR #1749 Git-hosted Markdown documentation fact ingestion.

## 1. Decision

Eshu may ingest rich office documents and documentation archives only through a
bounded, source-fact-only extraction boundary. The proposed first formats are:

- `.docx` as an Office Open XML word-processing package.
- `.xlsx` as an Office Open XML workbook package.
- `.pptx` as an Office Open XML presentation package.
- `.csv` and `.tsv` as deterministic delimited text.
- `.zip`, `.tar`, and `.tar.gz` as explicitly configured documentation
  containers.

Legacy `.xls` is covered separately but is not approved for production
extraction in the first implementation. It may emit only a classified
unsupported warning until an isolated legacy Office converter is reviewed.

Every extractor must emit source-neutral documentation facts. It must not write
canonical graph state, infer service ownership, or treat document prose,
spreadsheet cells, slide text, comments, formulas, or archive membership as
operational truth. Reducers and query surfaces own any later comparison,
admission, drift finding, or truth label.

## 2. Non-Goals

This design does not approve:

- production ingestion code
- hosted enablement or default-on runtime flags
- graph projection from Office, spreadsheet, deck, or archive facts
- macro-enabled Office formats such as `.docm`, `.xlsm`, or `.pptm`
- active content execution, formula evaluation, OLE/ActiveX execution,
  embedded-object extraction, or external relationship dereferencing
- OCR, image analysis, transcript generation, or LLM summarization
- unrestricted spreadsheet value dumping
- recursive archive extraction
- public upload or public retrieval of extracted source bytes

## 3. Source Truth Boundary

The extraction path remains:

```text
source artifact -> bounded extractor -> documentation facts -> fact store
  -> reducer/query comparison -> API/MCP bounded reads
```

Collectors and converters observe source artifacts and emit:

- `documentation_source`
- `documentation_document`
- `documentation_section`
- `documentation_link`
- optional `documentation_entity_mention`
- optional `documentation_claim_candidate`

Documentation claim candidates use `authority=document_evidence`. They prove
what a document said at a source revision; they do not prove the claim is true.

Archive-contained documents must preserve both identities:

- the outer source URI and content hash
- the normalized internal path and contained-file hash

The internal path is provenance, not filesystem authority. It must never be
used for writes outside the extraction sandbox.

## 4. Resource Limits

The first implementation must hard-code these defaults and make runtime config
able to lower them. Raising a limit requires a follow-up design update,
security review, and no-regression proof on the affected collector path.

| Limit | Office package | CSV/TSV | Documentation archive |
| --- | ---: | ---: | ---: |
| Source file bytes | 50 MiB | 25 MiB | 100 MiB |
| Expanded bytes | 128 MiB | 25 MiB | 256 MiB |
| Compression ratio | 100:1 | not applicable | 100:1 |
| Entries or package parts | 2,000 | not applicable | 1,000 |
| Nested containers | reject | not applicable | skip with warning |
| Per-file wall time | 30s | 10s | 60s aggregate |
| XML depth | 64 | not applicable | inherited by contained file |
| Section or slide count | 2,000 sections / 500 slides | not applicable | inherited by contained file |
| Sheet count | 100 sheets | not applicable | inherited by contained file |
| Rows sampled | 500 per workbook, 100 per sheet | 500 per file | inherited by contained file |
| Columns sampled | 200 per sheet | 200 per file | inherited by contained file |
| Stored text excerpt | 16 KiB per section or slide | 16 KiB per table summary | inherited by contained file |
| Cell value excerpt | 512 bytes per cell | 512 bytes per cell | inherited by contained file |

Extraction must count bytes while streaming. A preflight estimate is not enough:
the reader must stop as soon as the cumulative expanded-byte, entry-count,
ratio, row, column, token-depth, or timeout budget is exceeded.

ZIP and tar readers must reject or skip unsafe paths before opening member
content. All member names must normalize to local, slash-separated paths with
no absolute prefix, drive prefix, backslash escape, `..` segment, NUL byte, or
empty component.

## 5. Format Mapping

### 5.1 DOCX

`.docx` extraction reads only package metadata, relationships, and XML parts
needed for document structure:

- title and revision from source path, content hash, and safe package metadata
- heading hierarchy to `documentation_section.ordinal_path`
- paragraphs and table cell text to bounded section content
- tables as section metadata with row, column, and truncation counts
- links from relationship parts to `documentation_link`
- image alt text when present
- image binary presence, count, content type, and hash only

Comments and tracked changes are sensitive annotations. The default behavior is
to emit presence counts and warning classes only. Annotation text may be
enabled later only by explicit source configuration, bounded excerpts, ACL
proof, and security signoff.

### 5.2 XLSX, CSV, TSV, And XLS

`.csv` and `.tsv` use a deterministic text path with charset detection limited
to UTF-8, UTF-8 with BOM, and explicit configured encodings. Other encodings
produce `unsupported_encoding`.

`.xlsx` reads workbook, sheet, table, shared-string, formula, comment, and
relationship metadata under the Office package limits. It emits:

- one document fact per workbook revision
- one section fact per visible sheet summary
- optional section facts for named tables
- bounded row evidence with deterministic row ordinals
- column names, inferred coarse cell classes, and truncation counts

Spreadsheet values are not operational truth. Inventory rows, owner cells, or
dependency lists remain document evidence until reducers compare them with
stronger source evidence.

Formula bodies are not evaluated. The extractor records formula presence,
counts, cell anchors, and a formula hash. It may persist a normalized formula
excerpt only when the formula contains no external workbook reference, no
credential-looking literal, and fits the cell excerpt limit.

Hidden sheets are represented by name hash, sheet ordinal, hidden state, row
count, and column count. Their cell values are skipped by default.

Legacy `.xls` is an OLE Compound File binary format, not OOXML. The first
implementation must classify `.xls` as `unsupported_legacy_binary` and avoid
parsing cell bytes. Any future `.xls` support must use an isolated converter
with network disabled, temp storage limits, CPU and memory limits, no macro
execution, and fixture proof for corrupt CFB and macro-bearing files.

### 5.3 PPTX

`.pptx` extraction reads slide relationships and XML parts only. It emits:

- one document fact per deck revision
- one section fact per slide, ordered by slide index
- title, body text, table text, notes text, and link facts when visible
- image alt text when present
- image binary presence, count, content type, and hash only

Hidden slides are represented with hidden state and title hash. Their body and
speaker-note text are skipped by default unless an explicit source
configuration enables hidden-slide extraction and ACL proof is present.

Comments use the same annotation policy as `.docx`: presence and counts by
default, bounded text only after explicit opt-in and security signoff.

### 5.4 Archives

Archive ingestion is allowed only for explicitly configured source paths or
provider attachments. Generic repository discovery must not unpack archives by
default.

Archive extraction must be streaming or sandboxed. If a future implementation
must materialize a member to disk, it must use a private per-run temporary
directory, no executable file mode, no symlink following, a cleanup deadline,
and the same normalized-path containment check before and after opening the
file.

Initial archive formats:

- `.zip`
- `.tar`
- `.tar.gz`

Allowed contained document types:

- `.md`, `.mdx`, `.markdown`
- `.txt`
- `.csv`, `.tsv`
- `.docx`, `.xlsx`, `.pptx`
- `.json`, only when explicitly configured as documentation source input

Unsupported members are skipped with warning metadata. Symlinks, hard links,
device nodes, FIFOs, sockets, absolute paths, path traversal, nested archives,
macro-enabled Office files, executables, private-key files, credential bundles,
database files, and secret-store exports are always skipped.

Contained documents inherit the archive source ACL unless the source provider
reports a stricter member-level ACL. The contained document canonical URI must
include the archive source URI plus a normalized internal path. It must not
point at an extraction temp path.

## 6. Privacy, ACL, And Metadata Rules

Office internal author fields, last-modified-by fields, comment authors,
revision authors, custom properties, hidden-sheet names, and slide comments can
carry private identity data. The default durable shape is:

- presence booleans
- bounded counts
- stable fingerprints
- source-reported timestamps only when the source ACL was evaluated

Raw user names, email addresses, local filesystem paths, printer paths, network
shares, tenant IDs, credential strings, and private URLs must not be written to
logs, metrics, stable-ID identity maps, or public docs. If a value is useful
for correlation but sensitive, store a keyed or stable fingerprint and a coarse
kind.

ACL handling follows the documentation evidence packet contract:

- source ACL evaluated and allowed: facts may include bounded source-native
  content and API/MCP reads may return it through authorized routes
- source ACL evaluated and denied: facts may keep non-content metadata, but
  evidence reads deny content
- source ACL missing or partial: set `acl_summary.is_partial`, preserve the
  partial reason, and deny evidence-packet content by default

Archive-contained files never receive broader permissions than the outer
source artifact.

## 7. Failure Classes

The first implementation must preserve compact failure classes in
`source_metadata` or a follow-up `documentation_warning` fact if per-member
cardinality requires it. Accepted classes:

- `unsupported_format`
- `unsupported_legacy_binary`
- `unsupported_macro_enabled`
- `unsupported_encoding`
- `malformed_container`
- `malformed_xml`
- `resource_limit_exceeded`
- `compression_ratio_exceeded`
- `timeout`
- `archive_path_escape`
- `archive_symlink_skipped`
- `archive_special_file_skipped`
- `archive_nested_skipped`
- `credential_file_skipped`
- `sensitive_value_redacted`
- `annotation_text_skipped`
- `hidden_content_skipped`
- `acl_unknown`
- `partial_extraction`

Failures must be visible to operators through fact readback and status evidence.
They must not be swallowed as empty documents.

## 8. Fixture Matrix

| Issue | Required fixtures |
| --- | --- |
| #1738 DOCX | normal document, table-heavy document, comments, tracked changes, embedded images, malformed ZIP, malformed XML, zip-bomb-like package |
| #1739 spreadsheet | CSV, TSV, multi-sheet XLSX, named tables, formulas, hidden sheets, comments, large row count, malformed files, sensitive-looking cells, `.xls` unsupported binary |
| #1740 PPTX | ordinary deck, speaker notes, tables, links, hidden slides, comments, embedded images, malformed ZIP, malformed XML, large deck |
| #1747 archive | normal bundle, path traversal, symlink, special file, nested archive, compression bomb, credential-looking file, mixed supported and unsupported types |

Fixtures must be synthetic and repository-safe. Do not commit private
documents, customer names, private organization names, IP addresses, real
credentials, personal data, or proprietary vendor packets.

## 9. Implementation Sequencing

1. Add failing tests for the shared resource guard and fixture classifiers.
   Prove path normalization, byte accounting, decompression ratio, timeout,
   macro-bearing package rejection, and credential-file skips before writing
   extractors.
2. Implement CSV/TSV metadata and bounded row summaries first because they do
   not require container extraction.
3. Implement the shared OOXML package preflight and part allowlist. Keep it
   default off and prove `.docx`, `.xlsx`, and `.pptx` malformed and bomb-like
   fixtures before emitting durable facts.
4. Add format-specific mappers for DOCX, XLSX, and PPTX. Prove source,
   document, section, link, mention, and claim-candidate fact readback through
   existing documentation API/MCP routes.
5. Add archive preflight and contained-document routing only after standalone
   extractors pass. Keep archive ingestion explicit-opt-in and non-recursive.
6. Add status, logs, spans, and metrics for extraction attempts, failure
   classes, skipped members, expanded bytes, elapsed time, and truncation.
   Metric labels must stay bounded by source kind, format, and result class.
7. Run the security review gate. Hosted or repository ingestion stays disabled
   until the review records approval for resource limits, ACL behavior,
   metadata redaction, and fixture coverage.

## 10. Verification Gates

Every implementation PR under this design must include:

- failing regression tests first for the targeted format
- package tests for extractor, fact emission, query readback, and MCP parity
- fixture tests for malformed, oversized, and sensitive cases
- docs updates for any new user-visible format, env var, capability, route, or
  failure class
- `scripts/test-verify-collector-authoring-gate.sh`
- `scripts/verify-collector-authoring-gate.sh`
- `scripts/test-verify-performance-evidence.sh`
- `scripts/verify-performance-evidence.sh`
- docs build and `git diff --check`

No-Observability-Change: this note changes design documentation only. Future
runtime work must add or name operator-visible extraction signals before it can
claim readiness.

## 11. References

- Microsoft Open XML SDK overview:
  https://learn.microsoft.com/en-us/office/open-xml/open-xml-sdk
- Microsoft Open Packaging Conventions overview:
  https://learn.microsoft.com/en-us/previous-versions/windows/desktop/opc/open-packaging-conventions-overview
- Go `archive/zip` package path-safety behavior:
  https://go.dev/pkg/archive/zip/
- Go `archive/tar` package:
  https://go.dev/pkg/archive/tar/
- OWASP File Upload Cheat Sheet:
  https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html
