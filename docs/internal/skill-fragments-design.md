# Skill Fragments Design (S1)

This is the design doc for S1 of [Epic S: skillgen — single source of
truth, three host adapters, CI-enforced roundtrip](#3699). It defines the
`skill-fragments/` source-of-truth structure, enumerates the seven
Eshu-specific fragments, defines the fragment contract, the byte-citation
anchor convention, the 3-target-host matrix, and the adapter layer. It
contains no code and no tests. S2 implements; S3 gates.

## Context

Eshu's agent-facing surface today is hand-maintained across
[`AGENTS.md`](../../AGENTS.md) (generic),
[`CLAUDE.md`](../../CLAUDE.md) (Claude Code-specific), the nine project
skills in [`.agents/skills/`](../../.agents/skills/) (with
[`.claude/skills/`](../../.claude/skills/) and
[`.codex/skills/`](../../.codex/skills/) as symlinks), and the host install
guidance in [Assistant Guidance](../public/reference/assistant-guidance.md)
and [Assistant Fast-Path Hook
Contract](../public/reference/assistant-fast-path-hooks.md). The closed
install discipline is mature, but the **generation discipline is missing**.

This design inherits from the closed ritual and install work:

- [#2457](https://github.com/eshu-hq/eshu/issues/2457) (closed) — agent
  ritual: make Eshu the default an agent consults.
- [#2493](https://github.com/eshu-hq/eshu/issues/2493) (closed) — assistant
  ritual install contract for Claude Code, Codex, and Cursor.
- [#1767](https://github.com/eshu-hq/eshu/issues/1767) (closed) — `eshu mcp
  setup` with platform-specific safe install and verify modes.
- [#1825](https://github.com/eshu-hq/eshu/issues/1825) (closed) — component
  inventory and extension diagnostics through API, MCP, and CLI.
- [#3307](https://github.com/eshu-hq/eshu/issues/3307) (closed) — portable
  evidence graph bundle (`evidence_bundle.v1`); the bundle-reproduction
  fragment anchors here.

What #2457–#3307 did not deliver is the **single source of truth** plus
**deterministic generator** plus **roundtrip baseline** plus **CI gate** that
keeps every host skill current with the upstream rule. This doc defines
that.

## Problem

The agent-facing surface drifts. The local-first default in
[`docs/internal/agent-guide.md:120-146`](../../docs/internal/agent-guide.md)
(which the
[`local-first` fragment](#fragment-local-first) anchors against) may not be
mirrored in `.codex/skills/`. The reducer invariant wording in one skill
may not match another. When Eshu changes a rule, the change does not
propagate to every host's skill, and no CI gate catches the drift. The
hand-maintained file count grows with each new host or fragment; the
failure mode is silent, not loud.

## Goal

Adopt the discipline from Graphify's `tools/skillgen/` — **single source
of truth + deterministic generator + roundtrip baseline + CI gate** —
with Eshu-specific content that no peer project ships:

- A single `skill-fragments/` source of truth, one Markdown file per
  fragment with a stable YAML frontmatter contract.
- A deterministic Go generator that emits per-host skill files from the
  fragments, capability-aware, without an LLM provider key.
- A roundtrip baseline (`expected/`) committed next to the generator so
  any hand-edit fails the S3 CI gate with a clear diff.
- A 3-host adapter matrix: Claude Code, Cursor, Codex. v1 ships skills
  plus the always-on layer only; hooks are out of scope.

The seven fragments encode the Eshu-specific value. Each fragment has a
`byte_citation` anchor pointing at the canonical source of the rule so a
maintainer can verify the skill is current at any time.

## The Seven Fragments

Each fragment is one Markdown file under `skill-fragments/`. The
`byte_citation` column points at the canonical source of the rule the
fragment encodes; S2 must copy that anchor into the generated skill, and
S3 must verify the citation still resolves to live content.

| Fragment id | Purpose | Rule encoded | Canonical source | Byte-citation anchor |
| --- | --- | --- | --- | --- |
| `operating-standard` | The agent's role in Eshu's operating order. | Accuracy → Performance → Concurrency is fixed; runtime work MUST prove correctness before proving speed. | [`docs/internal/agent-guide.md:14-22`](../../docs/internal/agent-guide.md) and [`AGENTS.md:70-78`](../../AGENTS.md) | `docs/internal/agent-guide.md#14-22` |
| `truth-labels` | The wire-level truth contract. | API/MCP/CLI responses MUST carry a `truth` envelope; `exact` / `derived` / `fallback` are the only valid levels; high-authority capabilities MUST return `unsupported_capability` rather than silently downgrading. | [`docs/public/reference/truth-label-protocol.md:10-21`](../../docs/public/reference/truth-label-protocol.md) (levels) and `:108-127` (error codes) | `docs/public/reference/truth-label-protocol.md#10-21` |
| `capability-profiles` | What Eshu may claim in each runtime profile. | Profiles are `local_lightweight`, `local_authoritative`, `local_full_stack`, `production`; a profile row marked unsupported MUST return `unsupported_capability`; truth ceilings live in `specs/capability-matrix.v1.yaml` plus `go/internal/query/contract.go`. | [`docs/public/reference/capability-conformance-spec.md:29-49`](../../docs/public/reference/capability-conformance-spec.md) and `:53-62` (truth ceilings) | `docs/public/reference/capability-conformance-spec.md#29-49` |
| `reducer-invariant` | Intake never writes graph state; use MCP `dispatch_*.go` tools. | Intake services observe source truth and enqueue work; the resolution engine (reducer) is the only writer of canonical graph state. MCP tool calls MUST route through `dispatchTool` into the shared HTTP query handlers (`go/internal/mcp/dispatch.go:15`); queue claims are leased, retryable, supersedable, and dead-letterable. | [`docs/public/architecture.md:24-26`](../../docs/public/architecture.md) (the canonical invariant), [`go/internal/mcp/dispatch.go:15-21`](../../go/internal/mcp/dispatch.go) (`dispatchTool` routes to internal HTTP), [`go/internal/storage/postgres/AGENTS.md:49-78`](../../go/internal/storage/postgres/AGENTS.md) (ack atomicity, lease fencing, claim ordering) | `docs/public/architecture.md#24-26` |
| `local-first` | Eshu works without an LLM provider key. | Every fragment's default rendering MUST work in `local_lightweight` and `local_authoritative` profiles without a configured LLM provider; LLM augmentation is policy-gated; `llm:no-provider` is a first-class status, not an error. | [`docs/internal/agent-guide.md:120-146`](../../docs/internal/agent-guide.md) (Performance And Evidence section that anchors deterministic defaults), [`go/internal/semanticqueue/README.md:10`](../../go/internal/semanticqueue/README.md) (no-provider, policy-denied, budget-denied, unsafe, unchanged, changed as planner labels), [`docs/internal/design/3140-investigation-evidence-packet-v2.md:18,59-63`](../../docs/internal/design/3140-investigation-evidence-packet-v2.md) (no-provider deterministic contract) | `docs/internal/agent-guide.md#120-146` |
| `bundle-reproduction` | How to load, verify, and reproduce an `evidence_bundle.v1`. | A recipient can run `eshu evidence bundle validate` against a redacted `evidence_bundle.v1`; the bundle is share-safe (no private endpoints, credentials, raw prompts, or local paths); reproduce handles point at bounded CLI/API/MCP calls the recipient can run against their own instance. | [`docs/public/reference/evidence-bundle.md:1-49`](../../docs/public/reference/evidence-bundle.md) (full artifact shape), and [#3307](https://github.com/eshu-hq/eshu/issues/3307) | `docs/public/reference/evidence-bundle.md#1-49` |
| `per-collector-matrix` | Enumerate the active collector set and each collector's MCP surface, capability-aware. | The active collector set is whatever is enabled in the active runtime profile; per-collector MCP tools must be enumerated from the live capability catalog, not from a static prose list. The same fragment renders differently for a code-only deployment versus a full-stack deployment with Terraform + AWS + K8s. | [`#1825`](https://github.com/eshu-hq/eshu/issues/1825) (component inventory), [`go/internal/capabilitycatalog/catalog.go`](../../go/internal/capabilitycatalog/catalog.go) (canonical catalog source), [`docs/internal/agent-guide.md:41-56`](../../docs/internal/agent-guide.md) (collector ownership table) | `go/internal/capabilitycatalog/catalog.go#1-80` |

## Fragment Contract

Every fragment file is Markdown with a YAML frontmatter block at the top,
delimited by `---`. The frontmatter is the machine-parseable surface S2
loads; the Markdown body is the human- and skill-readable surface S2
emits.

### Frontmatter schema

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `id` | string | yes | Stable fragment id; matches the filename without `.md`. |
| `version` | semver | yes | Fragment schema version; bumped on breaking contract changes. |
| `requires` | list of strings | no | Capability ids (e.g. `code_search.exact_symbol`) or fragment ids (e.g. `truth-labels`) this fragment depends on. S2 must skip a fragment whose `requires` are unmet for the target host. |
| `byte_citation` | string | yes | The canonical source of the rule, formatted `path/to/file#start-end`. S2 copies this into the generated skill as a stable comment block. S3 verifies the citation points at live content. |
| `description` | string | yes | One-line purpose used for the skill's frontmatter `description` field and for capability-aware rendering. |

`byte_citation` accepts the shorthand `#start-end` (anchors a line range
in the same file) and the longhand `path/to/file#start-end` (anchors a
range in another file). S2 normalizes to longhand before emission.

### Worked example

The `operating-standard` fragment, full file:

```markdown
---
id: operating-standard
version: 1.0.0
requires:
  - truth-labels
byte_citation: docs/internal/agent-guide.md#14-22
description: |
  Eshu's operating order is fixed: accuracy, then performance, then
  concurrency. An agent MUST prove correctness before proving speed.
---

# Eshu Operating Standard

For runtime work the order is fixed:

1. **Accuracy** — persisted facts, graph truth, API/MCP/CLI truth, and
   fixture intent agree.
2. **Performance** — the correct path has a before/after or
   no-regression measurement on the same input shape.
3. **Concurrency** — idempotency, retry boundaries, claim ordering,
   transaction scope, conflict keys, and dead-letter behavior hold
   under intended worker counts.

If any one of the three fails, the work is not done.
```

The generator reads the frontmatter, copies the body into the host
adapter output, and stamps the `byte_citation` into a stable comment
block at the top of the emitted skill. S3 then re-resolves the citation
against the current `main` and fails the gate when the cited range no
longer contains the rule.

## Byte-Citation Anchor Convention

A byte-citation anchor is `path#start-end`, where `start` and `end` are
1-based, inclusive line numbers. Anchors are stable identifiers, not
prose. S2 must:

1. Parse the `byte_citation` field from every fragment's frontmatter.
2. Stamp it into the emitted skill as a top-of-file comment block, one
   line per citation, formatted:
   `<!-- eshu:byte-citation path#start-end -->`.
3. Preserve the block through regeneration. S3 must verify the citation
   points at live content on `main` at PR time.

S3 must reject a citation when:

- The cited file no longer exists at the cited path.
- The cited line range no longer contains the rule the fragment encodes
  (S3 keeps a small heuristic: the line range must contain at least one
  of the fragment's rule-bearing sentences, normalized).
- The cited line range has shrunk past the fragment's published
  `version` (forcing a version bump).

The anchor is the **drift detector**. If a refactor moves the rule, the
citation breaks and S3 forces a maintainer to either update the citation
or revert the move.

## Three-Target-Host Matrix

v1 targets exactly three hosts. Each row shows what the host's loader
expects, the always-on inclusion mechanism, the hook shape, and which
fragment attributes survive the loader's filter.

| Host | Skill file path | Always-on layer | Hook shape | Surviving attributes |
| --- | --- | --- | --- | --- |
| Claude Code | `.claude/skills/<name>/SKILL.md` (symlinks to `.agents/skills/<name>/SKILL.md` in this repo, per [`.claude/skills/`](../../.claude/skills/)) | `CLAUDE.md` (per [Assistant Guidance](../public/reference/assistant-guidance.md) § Target Files) | none in v1 | `name`, `description`, full body, `byte_citation` block |
| Cursor | `.cursor/rules/eshu.mdc` (per [Assistant Guidance](../public/reference/assistant-guidance.md) § Target Files; the rules directory is Cursor's always-on mechanism) | `.cursor/rules/eshu.mdc` (Cursor has no separate CLAUDE.md-style file; the rule IS the always-on layer) | none in v1 | `name` (renamed to rule id), `description` (renamed to rule summary), full body, `byte_citation` block |
| Codex | `.codex/skills/<name>/SKILL.md` (symlinks to `.agents/skills/<name>/SKILL.md` in this repo, per [`.codex/skills/`](../../.codex/skills/)) | `AGENTS.md` (per [Assistant Guidance](../public/reference/assistant-guidance.md) § Target Files) | none in v1 | `name`, `description`, full body, `byte_citation` block |

Notes on the matrix:

- The three file paths are exactly what each host's loader expects. S2
  writes to the symlinked paths so the source of truth stays in one
  place (`.agents/skills/`); the symlinks are committed today.
- The always-on layer is the file the host injects into every session as
  baseline guidance. Claude Code uses `CLAUDE.md`, Codex uses
  `AGENTS.md`, Cursor uses a project rule under `.cursor/rules/`. The
  skill body augments the always-on layer; it does not replace it.
- v1 has no hooks. PreToolUse, PostToolUse, and on-launch hooks are
  reserved for a follow-up under the [Assistant Fast-Path Hook
  Contract](../public/reference/assistant-fast-path-hooks.md). Each
  hook must clear the [Implementation
  Gate](../public/reference/assistant-fast-path-hooks.md#implementation-gate)
  before any default-on behavior is permitted.
- "Surviving attributes" names which frontmatter and body fields the
  host's loader actually surfaces to the model. Cursor renames `name`
  and `description` to fit its rule schema; the byte-citation block
  is a comment and survives in all three because comments are not
  loader-filtered.

## What Is NOT a Fragment

The adapter layer handles everything host-specific. The fragment body
must NOT contain:

- Per-host prose: anything that says "in Claude Code you should…" or
  "Cursor users must…". The fragment describes the rule; the adapter
  renders it.
- Per-host formatting quirks: YAML vs TOML frontmatter, `.mdc` rule
  headers, Cursor's globs/frontmatter fields. The adapter owns these.
- Per-host tooling differences: which host supports which hook, which
  host reads which directory, which host ignores which comment style.
  The matrix above is the canonical mapping; the fragment does not
  encode it.
- Generated boilerplate: file headers, install banners, "managed block"
  markers (e.g. the `<!-- BEGIN ESHU GUIDANCE -->` markers in
  [Assistant Guidance](../public/reference/assistant-guidance.md) §
  Managed Block). The adapter emits those.

This split is the discipline. A rule that needs to change because
Eshu's contract changed goes in a fragment. A rule that needs to
change because Claude Code changed its skill loader goes in an
adapter. Mixing the two is what causes drift.

## Do Not Copy Graphify

Graphify's `tools/skillgen/` is the **discipline inspiration**, not the
**code shape**. We adopt four ideas:

1. Single source of truth for the agent-facing surface.
2. Deterministic generator that emits per-host artifacts.
3. Roundtrip baseline committed next to the generator.
4. CI gate that fails when generated output drifts.

We diverge on every other axis:

- **Language.** Eshu is Go. The skillgen is Go. No Python, no YAML DSL,
  no Node.
- **Location.** The skillgen lives under [`go/cmd/skillgen/`](../../go/cmd/)
  alongside the other CLI wrappers in `go/cmd/` (e.g. `eshu`,
  `bootstrap-index`, `ingester`, `reducer`, `proof-of-value`,
  `capability-inventory`). The fragment parser and host adapter registry
  live under `go/internal/extensions/skillgen/`, matching the SDK
  pattern at `go/internal/collector/sdk/`.
- **Content.** Graphify emits generic AI-host skill text. Eshu emits
  fragments that encode the **Eshu-specific** value: truth labels
  (per [Truth Label Protocol](../public/reference/truth-label-protocol.md)),
  capability profiles (per [Capability Conformance
  Spec](../public/reference/capability-conformance-spec.md)),
  byte-citation as a content anchor, the reducer invariant (per
  [Architecture](../public/architecture.md) line 25), bundle
  reproduction (per [Evidence Bundle](../public/reference/evidence-bundle.md)
  and [#3307](https://github.com/eshu-hq/eshu/issues/3307)), and the
  per-collector matrix (per [#1825](https://github.com/eshu-hq/eshu/issues/1825)).
  None of those are Graphify-shaped.
- **Host count.** Graphify ships 18+ AI-host adapters. Eshu ships the
  discipline for three hosts and lets the community fill in more via
  the adapter registry. The value is the single source of truth, the
  roundtrip baseline, and the Eshu-specific content — not the host
  count.

## Acceptance Criteria

Mirroring the issue body:

1. `docs/internal/skill-fragments-design.md` exists and is the
   canonical design doc for S1.
2. The doc enumerates the seven Eshu-specific fragments with one-line
   purpose, the rule each encodes, and the source-of-truth `file:line`
   for each rule.
3. The doc defines the fragment contract (frontmatter schema) that S2
   will parse.
4. The doc defines the byte-citation anchor convention that S2 will
   preserve as a stable comment block in the generated skill, and that
   S3 will roundtrip-test.
5. The doc lists the 3-target-host matrix (Claude Code, Cursor, Codex)
   with skill file path, hook shape, always-on layer, and surviving
   attributes per host.
6. The doc explicitly excludes non-fragments (per-host prose, formatting
   quirks, host-specific tooling) from the fragment contract.
7. The doc cites [#3307](https://github.com/eshu-hq/eshu/issues/3307)
   (evidence bundle), [#2457](https://github.com/eshu-hq/eshu/issues/2457)
   (agent ritual),
   [#2493](https://github.com/eshu-hq/eshu/issues/2493) (assistant
   install),
   [#1767](https://github.com/eshu-hq/eshu/issues/1767) (mcp setup),
   [#1825](https://github.com/eshu-hq/eshu/issues/1825) (component
   inventory),
   [`docs/internal/agent-guide.md:120-146`](../../docs/internal/agent-guide.md)
   (Performance And Evidence section), and the truth-label, capability,
   telemetry, and architecture docs as upstream sources of the rules
   the fragments encode.
8. The doc includes the "do not copy Graphify" note explaining the
   discipline-vs-code-shape split.

## Non-Goals

Mirroring the issue body:

- Do not implement `skill-fragments/`. The design doc may sketch the
  layout but the directory does not need to exist after this issue
  closes.
- Do not implement `tools/skillgen/`. That is S2.
- Do not implement the CI gate. That is S3.
- Do not target more than three hosts. The matrix is Claude Code,
  Cursor, Codex only.
- Do not generate LLM-dependent skills. Every fragment's default
  rendering must work without an LLM provider key.
- Do not add AI attribution.

## Linked Work

Issues:

- [#2457](https://github.com/eshu-hq/eshu/issues/2457) (closed) — agent
  ritual policy.
- [#2493](https://github.com/eshu-hq/eshu/issues/2493) (closed) —
  assistant install contract for the three hosts.
- [#1767](https://github.com/eshu-hq/eshu/issues/1767) (closed) — mcp
  setup with platform-specific safe install.
- [#1825](https://github.com/eshu-hq/eshu/issues/1825) (closed) —
  component inventory and extension diagnostics.
- [#3307](https://github.com/eshu-hq/eshu/issues/3307) (closed) —
  portable evidence graph bundle (`evidence_bundle.v1`); the
  bundle-reproduction fragment anchors here.
- [#3696](https://github.com/eshu-hq/eshu/issues/3696) — this issue
  (S1).
- [#3697](https://github.com/eshu-hq/eshu/issues/3697) — S2: build
  `go/cmd/skillgen/`.
- [#3698](https://github.com/eshu-hq/eshu/issues/3698) — S3: CI gate.
- [#3699](https://github.com/eshu-hq/eshu/issues/3699) — parent epic.

Repo docs cited as canonical sources for the fragments:

- [`AGENTS.md`](../../AGENTS.md) — current generic agent-facing
  surface.
- [`CLAUDE.md`](../../CLAUDE.md) — current Claude Code-specific
  surface.
- [`.agents/skills/`](../../.agents/skills/) — current project skills
  (the 9 repository-owned skills the skillgen replaces with
  generated output).
- [`.claude/skills/`](../../.claude/skills/) and
  [`.codex/skills/`](../../.codex/skills/) — symlinks to
  `.agents/skills/`.
- [`docs/internal/agent-guide.md`](../../docs/internal/agent-guide.md)
  lines 14-22 (Operating Standard) and 120-146 (Performance And
  Evidence).
- [`docs/public/reference/truth-label-protocol.md`](../../docs/public/reference/truth-label-protocol.md)
  lines 10-21 (Truth Levels) and 108-127 (Error codes).
- [`docs/public/reference/capability-conformance-spec.md`](../../docs/public/reference/capability-conformance-spec.md)
  lines 29-49 (Runtime Profiles) and 53-62 (Truth ceilings).
- [`docs/public/reference/telemetry/index.md`](../../docs/public/reference/telemetry/index.md)
  (telemetry contract — referenced by `local-first` and
  `operating-standard`).
- [`docs/public/architecture.md`](../../docs/public/architecture.md)
  line 25 (canonical reducer-invariant sentence).
- [`docs/public/reference/evidence-bundle.md`](../../docs/public/reference/evidence-bundle.md)
  lines 1-49 (bundle shape).
- [`docs/public/reference/assistant-guidance.md`](../../docs/public/reference/assistant-guidance.md)
  § Target Files (host path mapping).
- [`docs/public/reference/assistant-fast-path-hooks.md`](../../docs/public/reference/assistant-fast-path-hooks.md)
  (hook contract — out of scope for v1, reserved for follow-up).
- [`go/internal/mcp/dispatch.go`](../../go/internal/mcp/dispatch.go)
  line 15 (`dispatchTool` definition).
- [`go/internal/storage/postgres/AGENTS.md`](../../go/internal/storage/postgres/AGENTS.md)
  lines 49-78 (ack atomicity, lease fencing, claim ordering,
  dead-letter semantics).
- [`go/internal/capabilitycatalog/catalog.go`](../../go/internal/capabilitycatalog/catalog.go)
  (canonical capability catalog source for the per-collector
  matrix).
- [`go/internal/semanticqueue/README.md`](../../go/internal/semanticqueue/README.md)
  line 10 (no-provider, policy-denied, budget-denied, unsafe,
  unchanged, changed planner labels).
- [`docs/internal/design/3140-investigation-evidence-packet-v2.md`](../../docs/internal/design/3140-investigation-evidence-packet-v2.md)
  lines 18 and 59-63 (no-provider deterministic contract).

## Flow Affected

`reducer → graph write → telemetry contract → agent-facing surface`.

The skillgen sits at the agent-facing boundary. The seven fragments are
the single source of truth for what an agent should know about Eshu.
The adapter layer emits host-shaped skills from those fragments. The
S3 CI gate enforces roundtrip integrity on every PR.

Concretely, the affected flow is:

```text
fragment (skill-fragments/*.md)
  → host adapter (.claude/, .cursor/, .codex/)
    → expected/ roundtrip baseline
      → S3 CI gate (scripts/verify-skill-roundtrip.sh)
```

A change to a fragment must propagate byte-identically to all three
host adapters and the baseline, or the S3 gate fails with a clear diff
and forces the maintainer to either accept the regeneration or restore
the fragment.
