# #4595 — Wrong-Answer Report Capture: Executable Slice Plan

Status: PLAN (not yet executed). Feature issue:
[#4595](https://github.com/eshu-hq/eshu/issues/4595), under epic #4389 (Ifá).

Goal: a user who sees a wrong answer runs `eshu report capture`, gets ONE
share-safe artifact (the "report bundle"), attaches it to a GitHub issue, and a
maintainer-confirmed bundle converts into an Odù skeleton so the wrong answer
becomes a permanent conformance case.

This plan is grounded in the tree as of `origin/main` (b69361d6e). Every
load-bearing claim cites file:line.

---

## Research findings (what the plan builds on)

### CLI surface

- Verbs are cobra commands registered via `init()` + `rootCmd.AddCommand`
  (`go/cmd/eshu/evidence_bundle_cmd.go:18-20`). The closest structural
  precedent is the existing portable evidence bundle verb family
  (`eshu evidence bundle export|validate`,
  `go/cmd/eshu/evidence_bundle_cmd.go:22-70`), backed by
  `go/internal/evidencebundle` (`types.go:10-21`, `validate.go:12-21`).
- Queries reach the API through `APIClient.GetEnvelope`/`PostEnvelope`
  (`go/cmd/eshu/client.go:80-93`), which request the canonical envelope MIME
  type (`client.go:17`) and decode per-verb `{data, truth, error}` structs
  (example: `go/cmd/eshu/trace.go:146-151`).
- `go/cmd/eshu` already imports `go/internal/query` directly
  (`go/cmd/eshu/admin_initial_credential_audit.go:12`), so the bundle can
  embed the real envelope types instead of copies.
- Top-level command name `report` is FREE: `report` exists only as a
  subcommand of `operator-digest` (`operator_digest_cmd.go:76`) and
  `first-run` (`first_run_evidence_test.go:319-323`); the service surface uses
  `service-report` (`service_report_cmd.go:28`).

### Truth envelope, truncation, evidence citation handles

- `query.TruthEnvelope{Level, Capability, Profile, Basis, Backend, Freshness,
  Reason}` — `go/internal/query/contract.go:97-105`; levels/bases/freshness
  enums at `contract.go:53-78`; `ResponseEnvelope{Data, Truth, Error}` at
  `contract.go:141-145`; `ErrorEnvelope` at `contract.go:132-139`.
- Truncation is a read-model field, not an envelope field (e.g.
  `AnswerPacket.Truncated`, `go/internal/query/answer_packet.go:88-89`;
  `admission_decision_types.go:126`). The bundle records the observed
  truncation flags from the data it captured, not a new contract.
- Evidence citation handle shape: `evidenceCitationHandle`
  (`go/internal/query/evidence_citation.go:33-43`), exported as
  `query.EvidenceCitationHandle` (`evidence_citation_public.go:10`). Full
  citations (`evidence_citation.go:54-76`) carry `content_hash`, `commit_sha`,
  provenance — and an `Excerpt` field, which is INLINE CONTENT BYTES and must
  be stripped from public bundles. Citation provenance is
  `{basis, rationale, source}` only (`evidence_citation_unified.go:17-21`) —
  citations do NOT carry fact IDs, and no public fact-record read route exists
  (checked `go/internal/query/openapi_paths_*.go`; only
  `/api/v0/documentation/facts` and the static schema-version registry). This
  drives the fact-ref design below.
- Citations hydration route: `/api/v0/evidence/citations`
  (`go/internal/query/openapi_paths_evidence.go:120`).

### The canonical redaction path (HARD requirement)

- The fact redaction rules live in `sdk/go/collector/validation.go`:
  - `sensitiveQueryPattern = (?i)(token|secret|password|credential|api[_-]?key|authorization)`
    (`validation.go:17`),
  - the exact-name allowlist `redactionSafePayloadKeys`
    (`validation.go:31-33`),
  - the recursive fail-closed key walk `validatePayloadKeys`
    (`validation.go:276-299`, "sensitive-looking key %q must be redacted
    before emission"),
  - the URI screen `validateSourceURI` (no host-local paths, no credentials,
    no sensitive query keys — `validation.go:249-267`).
- All of these are unexported. Reusing the SAME code path therefore requires a
  small exported surface in `sdk/go/collector` (Slice 1), not a copy.
- Ifá already ruled that redaction is key-name based and value-content masking
  does not exist (`docs/internal/design/4389-ifa-conformance-platform.md:745-748`
  and the shelved-obfuscation trigger at `:769-777`). The bundle inherits that
  posture: key-name redaction + payload-by-reference, never value masking.

### The Odù target (converter output)

- #4394 (Ifá P1) is CLOSED; the Odù shape landed:
  `ifa.Odu{Name, Work *projector.ScopeGenerationWork, Facts []facts.Envelope}`
  (`go/internal/ifa/odu.go:26-30`), canonicalized by `CanonicalizeOdu` through
  `replay.CanonicalizeValue` (`odu.go:33-48`) into the
  `{odu, schema_version, scopes[]}` document (`odu.go:38-42`), scopes/facts
  rendered at `odu.go:94-145`.
- Cataloged Odù are IN-CODE Go seeds (`catalog_seed.go:34-39`,
  `CatalogOdu{Odu, Detail}` at `catalog.go:10-15`), each requiring an
  honestly-green `specs/ifa-coverage-manifest.v1.yaml` row
  (`coverage_falsegreen_test.go` enforces this, per `catalog_seed.go:27-32`).
- #4572 is CLOSED; the fixture pack landed at `sdk/go/factschema/fixturepack`
  (`fixturepack.go:20-29`). Design correction on record: an Odù is NOT a
  fixture-pack entry; it is scenario-level and its facts must validate against
  the pack's schemas (composition —
  `4389-ifa-conformance-platform.md:320-332`). The issue text predates this
  correction; the converter targets the `ifa.Odu` scenario shape and validates
  payloads with `ifa.ValidateOduPayloads` + fixturepack schemas
  (`go/internal/ifa/README.md:46-52`).
- Replay/verdict machinery: `ifa.CanonicalizeOdu`, the roundtrip axis
  (`roundtrip.go`, `demoOrgRoundtripOdu`), and the `FactLoader` seam
  (`odu.go:21-23`) for loading facts from durable storage.

### Dependencies (#4592, #4593)

- #4593 (docs restructure) is CLOSED — the human path exists:
  `docs/public/guides/` + `docs/public/use/` with nav at
  `docs/mkdocs.yml:52-75`. NOTE: "Bundles" already means package registry
  bundles in the docs (`docs/public/guides/bundles.md:1-9`); the new artifact
  must consistently be called a "report bundle" to avoid collision.
- #4592 (first-run / demo corpus epic) is OPEN, but the demo-corpus artifacts
  the round-trip AC needs have landed: `docker-compose.demo.corpus.yaml`
  (manifest-declared subset of the golden-corpus fixture set, header lines
  2-8) and `specs/demo-first-answers.v1.yaml` (five pinned demo questions,
  each naming the MCP tool that fetches its answer, line 61). Only the
  Slice 2 END-TO-END round-trip test depends on this stack; capture (Slice 1)
  and the converter unit path are dependency-free.

---

## Bundle artifact schema

New package `go/internal/reportbundle` (sibling of, and convention-consistent
with, `go/internal/evidencebundle` — which stays untouched: it is an
operator-state demo bundle, a different artifact with a different lifecycle).

```
SchemaVersion = "wrong_answer_report.v1"

type Bundle struct {
    SchemaVersion string            `json:"schema_version"`  // fail-closed on mismatch, like evidencebundle/validate.go:14-15
    BundleID      string            `json:"bundle_id"`       // sha256 of canonical bundle content (deterministic)
    CreatedAt     string            `json:"created_at"`      // RFC3339, UTC
    ReporterNote  string            `json:"reporter_note"`   // what the user expected instead; key-name walked like everything else
    Query         CapturedQuery     `json:"query"`
    Response      CapturedResponse  `json:"response"`
    Evidence      EvidenceContext   `json:"evidence"`
    Redaction     RedactionProfile  `json:"redaction"`       // profile + rules applied (mirrors evidencebundle.RedactionProfile, types.go:37-40)
    Payloads      *PayloadAttachment `json:"payloads,omitempty"` // nil unless --include-payloads
    Validation    Validation        `json:"validation"`      // status + checks run (mirrors evidencebundle.Validation, types.go:99-102)
}

type CapturedQuery struct {
    Surface string         `json:"surface"`  // "api" | "mcp"
    Target  string         `json:"target"`   // endpoint path (no query string) or MCP tool name
    Method  string         `json:"method,omitempty"`
    Params  map[string]any `json:"params"`   // as issued; sensitive-named keys REDACTED (marker value), rule recorded
    Profile string         `json:"profile,omitempty"` // query profile in effect at capture
}

type CapturedResponse struct {
    Truth      *query.TruthEnvelope `json:"truth"`   // verbatim: level, profile, basis, backend, freshness, reason (contract.go:97-105)
    Error      *query.ErrorEnvelope `json:"error,omitempty"`
    Truncated  bool                 `json:"truncated"`        // observed read-model truncation flag(s)
    Data       json.RawMessage      `json:"data"`             // response data AFTER redaction walk (see below)
    DataDigest string               `json:"data_digest"`      // sha256 of replay-canonical data — the replay-equality anchor
}

type EvidenceContext struct {
    Citations     []CitationRef `json:"citations"`       // handle fields + citation_id/content_hash/commit_sha; Excerpt DROPPED unless --include-payloads
    FactRefs      []FactRef     `json:"fact_refs"`       // references ONLY — never payload bytes
    FactRefsState string        `json:"fact_refs_state"` // "resolved" | "unavailable"
    FactRefsReason string       `json:"fact_refs_reason,omitempty"`
}

type FactRef struct {                                    // field names mirror the Odù fact render, odu.go:118-134
    FactID        string `json:"fact_id"`
    StableFactKey string `json:"stable_fact_key"`
    FactKind      string `json:"fact_kind"`
    SchemaVersion string `json:"schema_version"`
    ScopeID       string `json:"scope_id"`
    GenerationID  string `json:"generation_id"`
}

type PayloadAttachment struct {                          // PRIVATE-TRIAGE ONLY
    Warning  string            `json:"warning"`          // fixed loud sentence, always serialized first
    Excerpts []CitationExcerpt `json:"excerpts,omitempty"`
    Facts    []facts.Envelope  `json:"facts,omitempty"`  // resolved fact envelopes, local captures only
}
```

Rationale for `Response.Data` being inline (redacted) rather than digest-only:
the truth-labeled response "actually returned" is what the maintainer triages,
and its content derives from facts that already passed the same key-name gate
at emission (`validation.go:284-285`); the bundle re-walks it anyway
(defense-in-depth) and the digest — computed over
`replay.CanonicalizeValue(data)` — is the replay-equality anchor. Fact
PAYLOADS, by contrast, are refs-only by default per the issue's hard
requirement.

## Redaction design (the hard requirement, explicitly)

Same code path, three mechanisms:

1. **Exported rules** (Slice 1, `sdk/go/collector`): a new small file
   `redaction.go` exporting
   - `IsSensitiveKeyName(key string) bool` — `sensitiveQueryPattern` match
     minus the `redactionSafePayloadKeys` exact-name allowlist (the very
     predicate `validatePayloadKeys` applies at `validation.go:284`), and
   - `ValidateShareSafeKeys(value any) error` — a thin exported wrapper over
     `validatePayloadKeys` (`validation.go:276-299`).
   The unexported internals move nothing and change nothing; existing
   callers (`validatePayload`, `validateSourceURI`) keep using them. This is
   an SDK API addition → `sdk/go/collector/CHANGELOG.md` entry and doc
   comments required (`eshu-contract-rigor` applies).
2. **Bundle-side redactor** (`reportbundle/redact.go`): walks every
   user-influenced or payload-adjacent region of the bundle (params, response
   data, reporter note, citation fields) and REPLACES values under
   sensitive-named keys with the fixed marker `"[REDACTED:key-name]"`,
   recording each applied rule in `Redaction.Rules`. Facts fail-closed at
   emission; a capture tool must not fail the user, so it redacts — but with
   the SAME predicate, so redactor and validator cannot disagree.
3. **Fail-closed gate on the finished artifact**: `reportbundle.Validate`
   deserializes the final bundle JSON and runs
   `collector.ValidateShareSafeKeys` over the entire document — literally the
   fact validator applied to the bundle. A public-profile bundle that trips it
   is a bug, and `eshu report capture` refuses to write it. When
   `--include-payloads` is set, the bundle's `Redaction.Profile` becomes
   `"private-triage"`, `Validation.Status` records that the share-safe gate
   was intentionally waived for the payload attachment only, and the rest of
   the bundle is still walked.

`--include-payloads` loudness: the flag's help text, a mandatory stderr
warning block on use, and `PayloadAttachment.Warning` inside the artifact all
state: private triage only, do not attach to public issues. `eshu report
validate` gains `--require-public`, which FAILS any bundle whose profile is
not share-safe (the issue-template instructions tell maintainers to run it).

How the canary proves it: the canary corpus plants sensitive-SHAPED key names
(`api_key`, `password`, `token`, `authorization_header`, `client_secret`,
`credential`) with unique sentinel VALUES in query params, response data, and
fact payloads. The test asserts (a) `ValidateShareSafeKeys` passes on the
serialized default bundle, (b) no sentinel value appears as a substring
anywhere in the serialized bundle bytes, (c) fact payload bytes are absent
(refs only), and (d) with `--include-payloads` the sentinels DO appear but the
profile is `private-triage`, `--require-public` fails it, and the stderr
warning fired. (b) is the teeth: it catches a redactor that renames keys but
leaks values, which (a) alone would miss.

---

## Slices (each its own PR, ordered by dependency)

### Slice 1 — `eshu report capture` + bundle schema + redaction + canary (dependency-free; lands first)

Scope: the versioned bundle artifact, capture against an API endpoint, the
shared redaction path, and `eshu report validate`.

Files to create:
- `sdk/go/collector/redaction.go` — exported `IsSensitiveKeyName`,
  `ValidateShareSafeKeys` (wrappers only; rules stay in `validation.go`).
- `go/internal/reportbundle/{doc.go,README.md,AGENTS.md}` — package docs
  (three-file rule, per repo Documentation Discipline).
- `go/internal/reportbundle/types.go` — schema above.
- `go/internal/reportbundle/capture.go` — build a Bundle from a
  `query.ResponseEnvelope` + request description; compute `DataDigest` via
  `replay.CanonicalizeValue` (`go/internal/replay`, the no-new-canonicalizer
  rule, `4389-...md:785-789`).
- `go/internal/reportbundle/redact.go` — the walker/marker described above.
- `go/internal/reportbundle/validate.go` — schema-version fail-closed +
  `collector.ValidateShareSafeKeys` gate + profile checks.
- `go/cmd/eshu/report_cmd.go` — `eshu report` parent, `capture` and
  `validate` subcommands, mirroring `evidence_bundle_cmd.go:22-70`
  conventions (SilenceUsage/SilenceErrors, `--out`/`--from`, stdin/stdout).

Files to modify: `sdk/go/collector/CHANGELOG.md`.

Command shape (Slice 1):

```
eshu report capture --endpoint /api/v0/... [--method GET|POST] [--params '<json>'] \
    [--note "expected X, got Y"] [--out report-bundle.json] [--include-payloads]
eshu report validate --from report-bundle.json [--require-public]
```

`--tool <mcp-tool-name>` is ACCEPTED in Slice 1 but records the tool name and
params verbatim with `Surface:"mcp"` and resolves the answer through the same
API read surface where a 1:1 route exists (the demo manifest maps each demo
question to its MCP tool, `specs/demo-first-answers.v1.yaml:61`); direct MCP
invocation from the CLI is out of Slice 1 (open question 5).

Fact refs in Slice 1: populated when the capture target is a local profile
where the CLI has durable-store access; otherwise
`FactRefsState:"unavailable"` with reason `"no public fact-record read
surface"` — resolution then happens maintainer-side in Slice 2 via the
`ifa.FactLoader` seam (`odu.go:21-23`). No new public API route.

TDD — failing tests first, in this order:
1. `go/internal/reportbundle/redact_test.go` `TestRedact_SensitiveKeyNamesUseCollectorRules`
   — table-driven against the same shapes `validation.go`'s own tests use;
   fails until `redact.go` + the SDK export exist.
2. `go/internal/reportbundle/capture_test.go` `TestCapture_RedactionCanary`
   — the canary described above (assertions a–d); THE acceptance-criterion
   test.
3. `go/internal/reportbundle/types_test.go` `TestBundle_SchemaRoundTrip`
   — encode→decode→re-encode byte-stable; unknown schema_version fails
   closed.
4. `go/cmd/eshu/report_cmd_test.go` `TestReportCapture_AgainstEnvelopeServer`
   — httptest server returns a canned `ResponseEnvelope` with truth +
   citations (incl. an `Excerpt`); assert truth verbatim, excerpt dropped,
   bundle validates; plus `TestReportValidate_RequirePublic`.
5. `sdk/go/collector/redaction_test.go` — exported predicate agrees with
   `validatePayloadKeys` on a shared fixture set (guards future drift).

Acceptance criterion satisfied: redaction canary. Also delivers the artifact
every other slice consumes.

Verification: `cd go && go test ./cmd/eshu ./internal/reportbundle -count=1`,
`cd sdk/go/collector && go test ./... -count=1`, lint, `make pre-pr`.
Runtime-affecting surface is a new offline CLI verb: declare
no-observability-change; no hot path touched.

### Slice 2 — bundle → Odù skeleton converter + round-trip (E2E leg needs the demo corpus)

Scope: `eshu report convert` turning a confirmed bundle into an Odù skeleton,
and the capture→convert→replay round-trip proof.

Files to create:
- `go/internal/ifa/reportconvert.go` — `OduSkeletonFromReport(bundle, loader)`:
  resolves `FactRefs` through the existing `FactLoader` seam
  (`odu.go:21-23`) or accepts an explicit facts slice (bundle payload
  attachment / cassette), builds `ifa.Odu{Name: "report:<bundle-id>", Facts: ...}`,
  validates via the existing `ValidateOduPayloads` + typed-decode axes
  (`ifa/README.md:46-52`) and fixturepack schemas, and emits:
  (1) the canonical Odù JSON via `CanonicalizeOdu` (`odu.go:33-48`),
  (2) a generated Go catalog-stub snippet + the required
  `specs/ifa-coverage-manifest.v1.yaml` row text (registration stays a
  manual maintainer act — matching both the in-code catalog reality,
  `catalog_seed.go:34-39`, and the issue's out-of-scope line on
  auto-conversion),
  (3) an `expected_answer` block carrying the bundle's `DataDigest` +
  query target, the assertion the maintainer corrects when encoding the
  RIGHT answer.
- `go/cmd/eshu/report_convert_cmd.go` — `eshu report convert --from
  bundle.json [--facts-from <cassette|dsn>] --out-dir <dir>`.
- `go/internal/ifa/reportconvert_test.go`, `go/cmd/eshu/report_convert_cmd_test.go`.

TDD — failing tests first:
1. `TestOduSkeletonFromReport_ValidBundleProducesValidOdu` — synthetic bundle
   + in-memory facts → skeleton passes `ValidateOduPayloads`; canonical JSON
   is idempotent (canonicalize twice, byte-equal).
2. `TestOduSkeletonFromReport_RefusesUnresolvedFacts` — refs that resolve to
   nothing fail closed with an actionable error (no silently-empty Odù).
3. `TestReportRoundTrip_DemoCorpus` (the AC test; gated like the golden-corpus
   gate, unique compose project + port overrides): stand up the demo stack
   (`docker-compose.demo.corpus.yaml`), issue one pinned demo question from
   `specs/demo-first-answers.v1.yaml` through `eshu report capture`, convert,
   replay the skeleton's facts, and assert the replayed answer's canonical
   digest equals the bundle's `DataDigest` — replay reproduces the reported
   answer.

Acceptance criterion satisfied: demo-corpus round-trip.

Dependencies: Slice 1 (the artifact). The E2E test needs the demo-corpus
stack (epic #4592 is open but the corpus artifacts are in-tree); unit tests
1–2 are dependency-free, so the PR can land with the E2E test wired into the
same lane that runs the golden gate rather than blocking on #4592's remaining
UX issues. Skills in play: `eshu-contract-rigor`, `eshu-golden-corpus-rigor`
(coverage-manifest row template), `golang-engineering`.

### Slice 3 — issue template + how-to docs (needs Slice 1 semantics only)

Scope: the reporting path a user actually sees.

Files to create:
- `.github/ISSUE_TEMPLATE/wrong-answer.yml` — GitHub issue form (existing
  precedent: `.github/ISSUE_TEMPLATE/competitive-audit.yml`). Fields: the
  attached `report-bundle.json` and nothing else (one optional free-text
  "what did you expect" line, mirroring the bundle's `reporter_note`), plus
  fixed instructions: run `eshu report capture` WITHOUT `--include-payloads`,
  and the maintainer-side `eshu report validate --require-public` check.
- `docs/public/guides/report-wrong-answer.md` — "Got a wrong answer?" —
  short how-to: capture, what the bundle contains (and deliberately does NOT
  contain), why it is public-safe by default, the loud `--include-payloads`
  caveat, attach to the wrong-answer issue form. Uses "report bundle"
  terminology throughout (the bare word "bundle" is taken:
  `docs/public/guides/bundles.md` means package registry bundles).

Files to modify:
- `docs/mkdocs.yml` — nav entry under the Use/guides section
  (`mkdocs.yml:52-75`).

TDD/verification: the docs build gate IS the failing-then-green proof for
this slice —
`uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
(nav entry added before the page exists fails strict; then goes green), plus
`git diff --check`. Issue-form YAML validated with the schema check used in
CI for `.github` workflow lint if present, else `yamllint`/`gh` preview.

Acceptance criteria satisfied: issue template; docs how-to page.

Dependencies: Slice 1 merged (flag names and command output must match the
docs). No demo-corpus dependency.

---

## Slice/dependency summary

| Slice | Scope (one line) | Depends on | AC covered |
|---|---|---|---|
| 1 | `eshu report capture`/`validate`, `wrong_answer_report.v1` schema, shared redaction path, canary | none | redaction canary |
| 2 | `eshu report convert` → Odù skeleton + demo-corpus round-trip | Slice 1; demo-corpus stack for the E2E test only | round-trip |
| 3 | wrong-answer issue form + "got a wrong answer?" guide | Slice 1 (naming/flags) | template + docs |

Out of scope, restated: no automated triage, no auto-registration of the
converted Odù (skeleton + manual catalog/manifest edit is deliberate), no
telemetry/phone-home; console/MCP capture affordances follow later.

## Open questions for the maintainer

1. **Package placement**: new `go/internal/reportbundle` (recommended; keeps
   `evidencebundle`'s operator-state artifact untouched) vs. growing
   `evidencebundle` with a second schema. The plan assumes the former.
2. **Response data inline vs. digest-only**: the plan includes redacted
   response `data` inline (triage value; already fact-redaction-derived) with
   the digest as the replay anchor. If the maintainer wants maximal caution,
   flip the default to digest+shape-only and gate inline data behind
   `--include-payloads` too — one-line change in `capture.go`, canary
   unchanged.
3. **Odù skeleton output location**: the converter writes to `--out-dir`
   (maintainer-chosen); should drafts have a conventional home (e.g.
   `go/internal/ifa/testdata/report-odu/`) so the catalog stub can point at a
   committed fixture, or stay wherever the maintainer runs triage? Depends on
   whether Ifá P2+ adds on-disk Odù loading (the "Drop-an-Odù" path,
   `4389-...md:737-748`).
4. **Fact refs on remote captures**: is `fact_refs_state:"unavailable"`
   acceptable for captures against a remote API (no public fact-record route
   exists today), with resolution deferred to the maintainer-side converter?
   The alternative — a new bounded citations→fact-refs read surface — is a
   real API addition and deliberately NOT in this plan.
5. **MCP capture depth in Slice 1**: record-the-tool-call (current plan) vs.
   an actual MCP client invocation from the CLI. The issue says CLI-first
   with MCP affordances to follow; the plan keeps direct MCP invocation out
   of Slice 1.
6. **Command naming**: `eshu report` (top level, free today) — confirm no
   planned collision with a future reporting surface before Slice 1 lands.
