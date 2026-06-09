# Google Workspace And External Export Documentation Ingestion Design

Status: **PROPOSED - SECURITY REVIEW AND IMPLEMENTATION PROOF REQUIRED.**

Refs: #1741, #1748, #1875. Parent: #1733. Related baseline: #1734 /
PR #1749, Git-hosted Markdown documentation fact ingestion and
`1738-office-spreadsheet-deck-archive-ingestion.md`.

## 1. Decision

Eshu may add two separate documentation evidence paths after security review:

- A live Google Workspace documentation collector for explicitly configured
  Google Docs, Sheets, and Slides.
- An offline/export ingestion path for explicitly supplied issue, ticket, and
  chat exports such as GitHub issue or discussion exports, Jira export files,
  Slack export archives, and Teams export payloads.

Both paths emit only source-neutral documentation facts:

- `documentation_source`
- `documentation_document`
- `documentation_section`
- `documentation_link`
- optional `documentation_entity_mention`
- optional `documentation_claim_candidate`

The facts record what an allowed source said at a source revision. They do not
create code, deployment, incident, work-item, service, ownership, or runtime
truth by themselves. Reducers, query surfaces, and documentation truth checkers
own later comparison, admission, drift findings, and truth labels.

The next Google Workspace implementation slice is #1875. It is limited to
mocked clients, synthetic IDs, default-off behavior, and fact readback proof.
It must not add live provider calls, chart options, Compose wiring, or public
operator docs.

## 2. Non-Goals

This design does not approve:

- production ingestion code or chart/runtime options
- default-on hosted collection for Google Workspace, GitHub, Jira, Slack, or
  Teams
- provider mutation APIs, document writes, comments, reactions, ticket updates,
  or chat replies
- broad Drive, organization, workspace, tenant, channel, project, repository,
  or user scans
- credential persistence in Postgres, graph stores, logs, metrics, fixtures,
  or evidence packets
- graph projection from document prose, ticket text, or conversation text
- treating issue or ticket exports as canonical work-item facts
- treating conversations as incident truth, deployment truth, ownership truth,
  or support-policy truth
- attachment binary extraction by default
- OCR, image analysis, audio/video transcript generation, or LLM summarization
- public upload, public retrieval, or public examples containing private source
  content, user emails, private URLs, tenant IDs, tokens, or hostnames

## 3. Source Truth Boundary

The shared boundary remains:

```text
allowed source -> collector or offline importer -> documentation facts
  -> fact store -> reducer/query comparison -> API/MCP bounded reads
```

Live provider collection and offline/export ingestion are different runtime
families:

| Path | Entrypoint | Credentials | Source authority | First approved use |
| --- | --- | --- | --- | --- |
| Google Workspace live | hosted claim-driven collector | read-only env or delegated runtime credential, never persisted | provider API metadata plus exported bytes | explicitly configured Docs, Sheets, Slides |
| Offline/export | local/importer or claim-driven file source | none, except optional local decrypt/unpack material outside Eshu | supplied export archive or file manifest | GitHub/Jira/Slack/Teams fixtures and operator-supplied exports |
| Future live issue/chat | separate provider collectors | provider-specific read-only env, never persisted | provider API metadata | follow-up security-reviewed designs only |

The offline parser may be shared with future live collectors only below a
provider wrapper that supplies source URI, source revision, ACL summary, and
redaction policy. A parser that accepts a ZIP, JSON, CSV, or TXT payload must
not infer provider ACLs from file names alone.

## 4. Explicit Allowlists

Blank configuration never means "all sources."

Google Workspace live collection must require at least one allowlist:

- `allowed_file_ids` for exact Docs, Sheets, or Slides file IDs.
- `allowed_folder_ids` for bounded folder descendants, with a max depth.
- `allowed_drive_ids` plus an explicit Drive query allowlist for shared drives.

The collector must reject `allDrives`, domain-wide search, user-root search, or
unbounded modified-time queries unless a later security review approves the
mode with a hard cap and an audit reason. Google's Drive `files.list` API
supports `user`, `domain`, `drive`, and `allDrives` corpora and recommends
`user` or `drive` over `allDrives` for efficiency. Eshu must therefore prefer
file IDs, bounded folder traversal, or `corpora=drive` plus `driveId` for
shared-drive scopes. Incomplete Drive search or rejected page tokens produce
partial evidence, not success.

Offline/export ingestion must require an import manifest with:

- `source_system` in an explicit enum: `github`, `jira`, `slack`, `teams`,
  `google_workspace_export`, or `generic_documentation_export`.
- `source_scope_id` such as repository, Jira project/export ID, Slack
  workspace/channel export ID, Teams team/chat export ID, or Drive export ID.
- explicit file paths inside the supplied archive or import directory.
- export creation timestamp and source-reported revision or cursor when
  available.
- an ACL policy: `source_acl_evaluated`, `source_acl_partial`, or
  `source_acl_unavailable`.

Archive traversal follows the office/archive design: no absolute paths, no
drive prefixes, no `..`, no symlinks, no special files, no nested archive
expansion by default, and no temp-path identity in facts.

## 5. Google Workspace Export Mapping

Local `.gdoc`, `.gsheet`, and `.gslides` files are pointer metadata, not source
content. They may be accepted only when they resolve to an explicitly
allowlisted Drive file ID. The collector must fetch provider metadata, verify
download/export capability, and export the source object to a reviewed document
format before document extraction.

Initial export mapping, aligned with Google's Workspace export MIME table:

| Workspace type | Preferred export | Existing extractor path |
| --- | --- | --- |
| Google Doc | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` (`.docx`), with `text/markdown`, HTML ZIP, or plain text only for explicit fallback tests | Office document ingestion |
| Google Sheet | `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` (`.xlsx`) for workbook structure; `text/csv` or `text/tab-separated-values` only for explicitly named first-sheet slices | Spreadsheet ingestion |
| Google Slide | `application/vnd.openxmlformats-officedocument.presentationml.presentation` (`.pptx`) | Deck ingestion |

The Drive export response is a bounded byte stream for a requested MIME type.
Google Workspace documents exported through `files.export` are provider-limited
to 10 MB, so implementation must enforce an Eshu-side byte limit at or below
that limit and classify over-limit responses as `resource_limit_exceeded`. If
Drive reports the file cannot be downloaded or exported, the collector emits
metadata-only facts with `download_not_allowed`.

## 6. Authentication And Credentials

Live collectors may read credentials only from process environment, mounted
secret files, or runtime identity providers already approved by deployment
policy. The first Google Workspace implementation should prefer one of:

- OAuth access with the narrowest approved Drive scope for explicitly shared
  files, such as per-file `drive.file` when the deployment can use a picker or
  explicit share flow.
- A service account or delegated identity limited to an explicit Drive, folder,
  or file allowlist.
- An external credential helper that returns an in-memory token and does not
  write a token cache under the Eshu workspace.

Google classifies broad Drive scopes such as `drive.readonly`,
`drive.metadata.readonly`, and `drive` as restricted scopes. A live collector
that needs those scopes must remain private or security-reviewed before any
runtime option ships. The design must reject an implementation that silently
upgrades from `drive.file` to restricted all-Drive scopes.

The collector must not persist refresh tokens, access tokens, service account
JSON, private keys, OAuth client secrets, or credential-helper output. It must
not log token claims, user emails, tenant domains, or raw private source URLs.
Credential fingerprints may use a stable hash over non-secret identity
metadata only when needed for operator diagnostics.

Offline/export ingestion must not require live credentials. If an operator
decrypts or downloads an export before ingestion, that step is outside Eshu and
must leave Eshu with a local file or object reference plus an ACL manifest. Eshu
does not store the decrypting key or download token.

## 7. ACL And Source Metadata

Every source and document fact must carry an ACL summary. Allowed durable values
are coarse and bounded:

- visibility: `private`, `restricted`, `shared_drive`, `workspace_public`,
  `public_link`, `unknown`, or provider-specific coarse equivalents
- reader/writer group fingerprints or stable group IDs when the source permits
  them
- user count summaries and user fingerprints only when security review approves
  the source
- `is_partial=true` plus `partial_reason` when the collector cannot prove the
  full ACL
- source policy flags such as `deleted`, `trashed`, `private_channel`,
  `direct_message`, `shared_channel`, `exported_by_admin`, and
  `attachment_metadata_only`

Raw user emails, display names, private URLs, channel names, tenant IDs, and
document bodies must not appear in logs, metric labels, public docs, fixture
names, or public evidence. If the value is needed for joins or de-duplication,
store a stable fingerprint and the provider identity kind.

Drive permission reads are paginated and can return a next-page token; failed
permission pagination must set `acl_partial` rather than dropping missing
permissions. Evidence packet reads must deny document body excerpts when ACL is
missing, partial without an approved policy, or denied for the requesting
principal.

## 8. Fact Mapping

### 8.1 Sources

`documentation_source` identifies the bounded source, not the whole provider:

| Provider/input | Source identity |
| --- | --- |
| Google Workspace file allowlist | `google_workspace:file-set:{hash}` |
| Google Workspace folder allowlist | `google_workspace:folder:{folder_id_hash}` |
| Google Workspace shared drive allowlist | `google_workspace:drive:{drive_id_hash}` |
| GitHub export | `github:{owner_repo_fingerprint}:{export_id}` |
| Jira export | `jira:{site_fingerprint}:{project_or_export_id}` |
| Slack export | `slack:{workspace_fingerprint}:{export_id}` |
| Teams export | `teams:{tenant_fingerprint}:{team_or_chat_export_id}` |

`source_metadata` should include provider type, import mode, export format,
allowlist kind, export timestamp, generator version when supplied, and bounded
counts. It must not include raw domains, raw emails, raw channel names, or raw
tokens.

### 8.2 Documents

`documentation_document` represents one source object revision:

- Google Docs: one document per Drive file revision/version and export MIME
  type.
- Google Sheets: one document per workbook revision; sheets may become
  sections.
- Google Slides: one document per deck revision; slides may become sections.
- GitHub issue/discussion export: one document per issue, pull request issue,
  or discussion thread.
- Jira export: one document per issue/work item in the export, as
  documentation evidence only.
- Slack export: one document per channel thread, direct-message thread, or
  single-message conversation slice, depending on export shape.
- Teams export: one document per channel root message plus replies, chat
  thread, or meeting chat slice.

`revision_id` must come from provider revision/version, export timestamp plus
source cursor, or a content hash when no source revision exists. `canonical_uri`
may be source-native only when the URI is redacted-safe; otherwise store a
fingerprint and keep the raw URI out of facts.

### 8.3 Sections

`documentation_section` preserves bounded provenance:

- Docs headings, paragraphs, and tables map to ordered sections.
- Sheets visible sheets and named tables map to sections with row/column counts
  and bounded excerpts.
- Slides map one section per slide, with speaker notes disabled by default.
- GitHub and Jira issue bodies, comments, timeline events, and changelog slices
  map to ordered sections.
- Slack and Teams root messages, replies, edits, deleted-message markers,
  reactions, and attachment references map to ordered sections.

Sections must preserve source start/end references, ordinal path, excerpt hash,
content format, truncation status, and warning status. Deleted or edited
conversation content remains evidence about the export state and must carry
source metadata such as `message_deleted`, `message_edited`, or
`edit_history_present` when supplied.

### 8.4 Links

`documentation_link` captures source-observed references:

- Drive export links and embedded document links.
- Issue, comment, discussion, Jira, Slack, and Teams permalink references when
  redaction policy permits.
- Attachment references as metadata-only links unless binary extraction is
  explicitly enabled by a future review.

Links with token-bearing query strings, signed URLs, or private download tokens
are redacted to kind, host fingerprint, and target hash. The raw target is not
stored.

### 8.5 Mentions And Claims

`documentation_entity_mention` and `documentation_claim_candidate` remain
optional deterministic outputs. They may be emitted only when exact structured
hints or exact links support them. Broad prose inference is not allowed.

Allowed claim candidate families for the first implementation:

- explicit local path, HTTP endpoint, CLI command, environment variable,
  container image, Terraform address, service/deployment relationship, and
  owner claim families already supported or planned by `doctruth`
- provider-specific source references such as GitHub issue number, Jira issue
  key, Slack message timestamp, or Teams message ID as documentation evidence
  anchors, not canonical truth

Every claim candidate must use `authority=document_evidence`, preserve
document, revision, section, and excerpt hash provenance, and stay suppressed
when subject or object mentions are ambiguous or unmatched.

## 9. Failure Classes

The implementation must emit compact failure classes in source metadata,
warning facts, status rows, or logs with bounded labels:

- `allowlist_required`
- `allowlist_empty`
- `allowlist_unsupported_scope`
- `auth_missing`
- `auth_denied`
- `auth_scope_too_broad`
- `credential_helper_failed`
- `permission_denied`
- `acl_unavailable`
- `acl_partial`
- `source_not_found`
- `source_deleted`
- `source_trashed`
- `source_revision_stale`
- `export_manifest_invalid`
- `export_source_mismatch`
- `export_format_unsupported`
- `export_archive_malformed`
- `export_path_escape`
- `export_private_channel_metadata_only`
- `attachment_metadata_only`
- `download_not_allowed`
- `provider_rate_limited`
- `provider_quota_exceeded`
- `pagination_incomplete`
- `resource_limit_exceeded`
- `timeout`
- `content_redacted`
- `sensitive_value_redacted`
- `duplicate_source_item`

Provider-specific errors must be normalized into these classes before they
enter metrics. Raw provider error bodies may contain private content and must
not be used as metric labels.

## 10. Telemetry And Evidence

No-Observability-Change: this commit is docs-only and introduces no runtime
behavior, metrics, spans, logs, status fields, queues, or API surfaces.

Any implementation PR must add or reuse operator-facing signals before runtime
enablement:

- collection duration and request counters by `source_system`, `collector_kind`,
  bounded `operation`, bounded `result`, and status class
- facts emitted and committed through existing fact counters
- warning/failure counters by bounded failure class
- rate-limit, quota, retry, and pagination-incomplete counters
- ACL partial/denied counters
- document, section, link, mention, and claim counts
- shared `collector.observe`, `fact.emit`, and durable commit spans
- status rows that expose generation ID, source scope, last success, last
  failure class, and partial-sync state

High-cardinality values such as file IDs, issue keys, channel IDs, user IDs,
message IDs, source URIs, titles, and hashes belong in trace attributes, logs,
or fact payloads after redaction policy, not in metric labels.

## 11. Fixture And Mocked-Client Matrix

The first implementation must land failing tests first, then source or parser
code. Required fixture or mocked-client cases:

| Area | Required cases |
| --- | --- |
| Google Docs | doc export success, missing permission, deleted/trashed doc, stale revision, export over size limit |
| Google Sheets | visible sheet, hidden sheet metadata-only, named table, rate limit, ACL partial |
| Google Slides | visible slide, hidden slide metadata-only, speaker notes disabled, download denied |
| Google Drive listing | file allowlist, folder allowlist, shared drive allowlist, incomplete search, unbounded query rejected |
| GitHub export | issue body/comments/timeline, discussion body/comments, deleted author or redacted user, private repository metadata |
| Jira export | issue body/comments/changelog, attachment reference, project metadata, export missing attachments, unsupported app data |
| Slack export | public channel thread, private channel metadata, DM metadata, edited message, deleted message, attachment link |
| Teams export | channel root/replies, chat thread, deleted-message window, reactions, attachment metadata, pagination next link |
| Generic export security | malformed manifest, archive path escape, nested archive skipped, oversized file, token-bearing URL redaction |
| Claim gating | exact mention emits claim, ambiguous mention suppresses claim, unmatched subject suppresses claim |

Live provider tests must use mocked clients and synthetic IDs only. Export
fixtures must be hand-written or generated synthetic samples with no real user
content, tenant identifiers, private URLs, emails, or tokens.

## 12. Implementation Sequence

1. Land this design note and get security review agreement on source scope,
   credential model, ACL shape, redaction, and fixture policy.
2. Implement #1875 with mocked Google Drive clients, synthetic IDs, explicit
   allowlist validation, export mapping, ACL summaries, and documentation fact
   readback while remaining default-off and runtime-free.
3. Add shared documentation export/import manifest types and parser tests with
   synthetic GitHub, Jira, Slack, and Teams export fixtures.
4. Implement offline/export ingestion first, emitting documentation facts only
   and no runtime chart option.
5. Implement the Google Workspace live collector behind disabled local config,
   with credentials read only from env or runtime identity.
6. Add telemetry and status rows for any live collector or claim-driven importer
   before enabling long-running runtime use.
7. Run focused package tests, collector authoring gates, docs build, and
   `git diff --check`.
8. Request security review before exposing any Helm, Compose, or public docs
   runtime option.
9. After security approval, add public operator docs, config examples with
   placeholder values only, and chart/Compose defaults that remain disabled.

## 13. Official API References

- Google Drive API download and export guide:
  https://developers.google.com/workspace/drive/api/guides/manage-downloads
- Google Workspace export MIME types:
  https://developers.google.com/workspace/drive/api/guides/ref-export-formats
- Google Drive `files.list` reference:
  https://developers.google.com/workspace/drive/api/reference/rest/v3/files/list
- Google Drive API scopes:
  https://developers.google.com/workspace/drive/api/guides/api-specific-auth
- Google Drive permissions listing:
  https://developers.google.com/workspace/drive/api/reference/rest/v3/permissions/list

## 14. Security Review Gates

Security review must explicitly sign off before production ingestion on:

- allowed source systems and source modes
- whether Google Drive restricted scopes are prohibited, allowed only with
  external verification evidence, or allowed only for private deployments
- credential source, token lifetime, and proof that no credential is persisted
- allowlist schema and proof that blank means disabled, never all
- ACL summary fields and any user or group fingerprinting
- redaction of emails, raw domains, private URLs, signed URLs, token-bearing
  links, issue keys if sensitive, channel names, and tenant identifiers
- fixture generation and fixture review for private-data absence
- attachment policy, including metadata-only default
- deleted/edited message handling and retention-window representation
- logging, metric labels, trace attributes, status payloads, and evidence packet
  content exposure
- chart, Compose, and public docs text before any live runtime option ships

Until those gates pass, the issues remain research/design only.
