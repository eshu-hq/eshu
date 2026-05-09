# Documentation Fact Schema Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the source-neutral documentation fact schema that documentation collectors will emit before Confluence, Git Markdown, and other documentation sources get runtime collectors.

**Architecture:** Keep the schema in `go/internal/facts` as additive payload contracts and fact-kind constants. Eshu core records documentation as observed evidence only; documentation facts never override operational graph truth and downstream drift findings must consume them as evidence inputs.

**Tech Stack:** Go 1.26, `go test`, MkDocs documentation.

---

### Task 1: Add Source-Neutral Payload Tests

**Files:**
- Create: `go/internal/facts/documentation_test.go`

**Step 1: Write the failing tests**

Add tests for:

- Confluence and Git Markdown documents use the same `DocumentationDocumentPayload`.
- document sections carry stable source-neutral identity and excerpt hashes.
- entity mentions support exact, ambiguous, and unmatched candidates.
- claim candidates are explicitly non-authoritative documentation evidence.
- stable IDs do not include source-specific display names.

Use table-driven tests with concrete payload values. Avoid mocks.

**Step 2: Run tests to verify RED**

Run:

```bash
cd go && go test ./internal/facts -run 'TestDocumentation' -count=1
```

Expected: FAIL because the documentation payload types and constants do not exist.

### Task 2: Implement Documentation Fact Payload Types

**Files:**
- Create: `go/internal/facts/documentation.go`
- Modify: `go/internal/facts/README.md`

**Step 1: Add fact-kind constants**

Add constants:

- `DocumentationSourceFactKind = "documentation_source"`
- `DocumentationDocumentFactKind = "documentation_document"`
- `DocumentationSectionFactKind = "documentation_section"`
- `DocumentationLinkFactKind = "documentation_link"`
- `DocumentationEntityMentionFactKind = "documentation_entity_mention"`
- `DocumentationClaimCandidateFactKind = "documentation_claim_candidate"`
- `DocumentationFactSchemaVersion = "1.0.0"`

**Step 2: Add payload structs**

Add source-neutral structs:

- `DocumentationSourcePayload`
- `DocumentationDocumentPayload`
- `DocumentationSectionPayload`
- `DocumentationLinkPayload`
- `DocumentationEntityMentionPayload`
- `DocumentationClaimCandidatePayload`
- supporting `DocumentationOwnerRef`, `DocumentationACLSummary`, and `DocumentationEvidenceRef`

Keep all fields serializable, source-neutral, and explicit. Use string slices
and maps for extensible metadata. Do not add database I/O or package state.

**Step 3: Add stable identity helpers**

Add helper functions that call `StableID`:

- `DocumentationSourceStableID`
- `DocumentationDocumentStableID`
- `DocumentationSectionStableID`
- `DocumentationEntityMentionStableID`
- `DocumentationClaimCandidateStableID`

Identity inputs must use durable source IDs, external IDs, revision IDs,
section anchors, text hashes, and claim hashes. They must not use display names
or mutable titles as the only identity input.

**Step 4: Run tests to verify GREEN**

Run:

```bash
cd go && go test ./internal/facts -run 'TestDocumentation' -count=1
```

Expected: PASS.

### Task 3: Add Scope And Workflow Contract Hooks

**Files:**
- Test: `go/internal/scope/scope_test.go`
- Test: `go/internal/workflow/collector_contract_test.go`
- Modify: `go/internal/scope/scope.go`
- Modify: `go/internal/workflow/collector_contract.go`

**Step 1: Write failing tests**

Add tests proving:

- `scope.KindDocumentationSource` validates as a scope kind.
- `scope.CollectorDocumentation` validates as a collector kind.
- the workflow collector contract has an entry for documentation collectors.
- documentation collectors do not require canonical operational graph keyspaces in this schema-only slice.

Run:

```bash
cd go && go test ./internal/scope ./internal/workflow -run 'Test.*Documentation' -count=1
```

Expected: FAIL because the constants and workflow contract do not exist.

**Step 2: Implement minimal constants and contract**

Add:

- `KindDocumentationSource ScopeKind = "documentation_source"`
- `CollectorDocumentation CollectorKind = "documentation"`

Add a workflow collector contract for `CollectorDocumentation` with no
canonical operational keyspaces yet. This records collector-family identity
without pretending documentation facts have reducer-owned graph projection in
this issue.

**Step 3: Run tests to verify GREEN**

Run:

```bash
cd go && go test ./internal/scope ./internal/workflow -run 'Test.*Documentation' -count=1
```

Expected: PASS.

### Task 4: Prove Persistence Compatibility

**Files:**
- Test: `go/internal/storage/postgres/facts_test.go`

**Step 1: Write failing or confirming test**

Add a test that persists one `documentation_document` envelope with:

- semantic schema version `1.0.0`
- collector kind `documentation`
- source confidence `observed`
- payload containing source-neutral document fields

Assert the fact store uses normal fact envelope persistence and does not need
documentation-specific columns.

**Step 2: Run the test**

Run:

```bash
cd go && go test ./internal/storage/postgres -run 'Test.*Documentation' -count=1
```

Expected: PASS if generic fact persistence already supports the payload, or
FAIL for a real compatibility gap.

**Step 3: Implement only if RED exposes a gap**

If the test fails because generic fact persistence rejects valid documentation
facts, make the minimal persistence fix and rerun the test.

### Task 5: Document Schema And Truth Boundaries

**Files:**
- Modify: `docs/docs/reference/fact-envelope-reference.md`
- Modify: `docs/docs/reference/fact-schema-versioning.md`
- Modify: `docs/docs/guides/collector-authoring.md`
- Modify: `go/internal/facts/README.md`

**Step 1: Add documentation fact schema reference**

Document the documentation fact kinds, schema version, source-neutral payload
families, and source confidence expectations.

**Step 2: Add truth-boundary warning**

State explicitly that documentation facts are evidence about what a document
says. They do not override graph truth, deployment truth, runtime truth, or
source-code truth.

**Step 3: Run docs build**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS.

### Task 6: Run Full Targeted Validation

**Files:**
- No edits.

**Step 1: Run Go tests**

Run:

```bash
cd go && go test ./internal/facts ./internal/scope ./internal/workflow ./internal/storage/postgres -count=1
```

Expected: PASS.

**Step 2: Run docs build**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS.

**Step 3: Update GitHub issue**

Comment on #64 with validation evidence and any explicit deferrals, especially
that collector runtime, claim extraction implementation, drift findings, and
evidence packet APIs remain in child issues #67, #68, #65, and #71.
