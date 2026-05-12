# ADR: Capture Composite Attribute Values For SchemaKnown Paths In The Terraform-State Parser

**Date:** 2026-05-12
**Status:** Accepted — open questions resolved 2026-05-12; implementation tracked by this PR.
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-20-terraform-state-collector.md` — owns the streaming parser and
  the memory contract this ADR extends.
- `docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md` —
  the drift design this ADR closes the last gap on (bucket E
  `attribute_drift`).
- Issue: `#187`
- Prior bug-fix commits on the same branch:
  - PR `#204` — locator hash alignment.
  - Commit `ff4c9ed` on this branch — Scope 2 redaction-rules wiring.
  - Commit `ed4903f` on this branch — Scope 3 provider-schema trust through
    attribute classification.
- This ADR closes the fifth bug surfaced by the Tier-2 verifier: the parser
  drops composite attribute values even when the schema covers them.

---

## Context

The Terraform-state parser at
`go/internal/collector/terraformstate/json_token.go:28-37`
(`readScalarOrSkip`) deliberately skips composite values via `skipNested`:

```go
func readScalarOrSkip(decoder *json.Decoder) (any, bool, error) {
    token, err := decoder.Token()
    if err != nil {
        return nil, false, err
    }
    if delim, ok := token.(json.Delim); ok {
        return nil, false, skipNested(decoder, delim)
    }
    return token, true, nil
}
```

That skip behavior is load-bearing for two memory invariants enforced by
`parser_memory_test.go`:

- `TestParserStreamingPathDoesNotCallJSONUnmarshal` (lines 29-52) asserts
  the streaming parser path never calls `encoding/json.Unmarshal`. A
  whole-subtree Unmarshal would allocate the entire composite value into
  the heap, breaking the streaming guarantee that holds peak heap growth
  under `maxStreamResourcePeakHeapGrowth = 48 MB` (line 25) for a 20k-
  instance state.
- `TestParserLargeStateStreamsIgnoredTopLevelPayload` (line 70) caps peak
  heap growth at `maxIgnoredPayloadPeakHeapGrowth = 24 MB` (line 24) when
  the parser skips large top-level keys like `checks`.

After Scope 2 (`ff4c9ed`) wired a versioned `redact.RuleSet` and Scope 3
(`ed4903f`) threaded `ProviderSchemaResolver` into attribute
classification, the redact policy now correctly returns `ActionPreserve`
for schema-known composites such as
`aws_s3_bucket.server_side_encryption_configuration`. But the value the
classifier preserves is `nil`, because the parser already discarded the
JSON subtree by the time the classifier ran. The Tier-2 verifier asserts
bucket E (`attribute_drift`) via:

```text
config side: server_side_encryption_configuration { rule { apply_server_side_encryption_by_default { sse_algorithm = "AES256" } } }
state side : server_side_encryption_configuration[0].rule[0].apply_server_side_encryption_by_default[0].sse_algorithm = "aws:kms"
```

`stateRowFromCollectorPayload` in
`go/internal/storage/postgres/tfstate_drift_evidence.go` (proven by
`tfstate_drift_evidence_test.go:400-435`) flattens the
nested-singleton-array shape into dot-paths like
`server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm`.
That flattener assumes the state-side `attributes` map has the actual
nested value. Today the value is `nil`, so the flattener finds nothing
and the drift handler has no state-side leaf to compare to config.
Bucket E cannot fire.

Three sequential gates failed in production this way, each silenced by
the next-in-line gate also being closed:

1. `RuleSet.version == ""` -> redact every scalar, drop every composite.
2. `SchemaUnknown` hardcoded -> same outcome under a different reason.
3. Parser skipped composites -> even with both gates open, the value is
   nil.

PR #204 fixed locator hashing. Commit `ff4c9ed` opened gate 1. Commit
`ed4903f` opened gate 2. This ADR commits to opening gate 3.

## Decision Drivers

1. **Bucket E correctness is a product contract.** Tier-2 ships when
   buckets A, B, D, and E all fire on a real collector chain. Without
   bucket E, drift detection cannot surface SSE misconfiguration, ACL
   drift on nested-block resources, or any other repeated-block attribute
   change. That is half the value of `attribute_drift`.

2. **The streaming memory budget is non-negotiable.** The 48 MB ceiling
   on 20k instances is a regression gate. Anything that allocates the
   whole state file or even the whole composite payload for non-schema
   attributes (an unbounded SCIP index, an inline policy doc, a
   provider-specific debug blob) breaks the contract.

3. **Schema depth is bounded; value depth is not.** Provider schemas
   declare nested-block structures with fixed depth. `aws_s3_bucket` has
   `server_side_encryption_configuration.rule.apply_server_side_encryption_by_default`
   (three levels). `aws_lambda_function.environment.variables` (two
   levels). The deepest nested-block in AWS provider 5.100.0 has 6 levels.
   Capturing the value of a schema-known composite is bounded by the
   schema. Capturing the value of a schema-unknown composite is bounded
   only by the file shape.

4. **The redact API contract must stay coherent.** `RuleSet.Classify`
   takes `(source, schemaTrust, fieldKind)` and returns an action. If the
   parser only captures values for some schema-known composites, the
   redact API's `ActionPreserve` for a composite would be a soft promise
   that the parser can break. The cleanest contract is: when the parser
   has schema coverage AND the rule set permits preservation, the value
   is captured; otherwise the value is dropped (and the redact API never
   sees a preserved-but-nil composite).

5. **The fix lives where the bug lives.** The skip happens in
   `terraformstate/attributes.go` and `json_token.go`. The schema-trust
   check is already in `attributes.go:schemaTrust`. Adding the capture
   path in the same file keeps the ownership boundary intact.

## Considered Options

### Option A — Streaming nested walker for SchemaKnown paths only

When `readAttributeValues` reaches a composite key whose
`(resourceType, key)` is schema-known, the parser walks the nested JSON
tokens recursively and builds a `map[string]any` / `[]any` structure
through the same streaming decoder it already uses. Unknown composites
still take the `skipNested` path.

The walker emits the same nested-singleton-array shape the loader's
flattener expects (matching
`tfstate_drift_evidence_test.go:400-435`).

**Pros**

- Streaming decoder stays in use. No `encoding/json.Unmarshal` call.
  `TestParserStreamingPathDoesNotCallJSONUnmarshal` continues to pass.
- Memory bounded by schema depth, not file depth. Worst-case composite
  is a few KB per schema-known attribute.
- Per-leaf classification stays possible: the walker can call
  `RedactionRules.Classify` on each scalar leaf it lands on, so a
  nested sensitive key inside a schema-known composite still gets
  redacted (the existing `isSensitiveSource` segment match in
  `policy.go:160-178` already handles dotted source paths).
- Unknown composites still skip; the existing memory budget for the
  `checks` top-level payload and for any provider-specific debug blob
  is preserved unchanged.

**Cons**

- New code path. The walker is roughly 80-120 lines in `attributes.go`
  plus tests. Edge cases include nested arrays, nested objects, mixed
  scalar/composite siblings, and empty arrays.
- The walker has to choose a representation. Terraform-state composites
  are always `[]any` of length 1 wrapping a `map[string]any` for
  singleton blocks, but multi-element blocks (`ingress[]`) need the
  array preserved so PR #198's first-wins truncation still applies.
  Representation is "raw JSON shape, parsed token-by-token" — same
  shape the loader's flattener already accepts.

### Option B — `decoder.Decode(&v any)` for SchemaKnown paths only

When a composite is schema-known, the parser calls
`decoder.Decode(&value)` to decode the full subtree into a `map[string]any`.
Memory grows by the composite size, bounded by the schema-known
allowlist not by the file.

**Pros**

- Simplest possible code change. Maybe 15 lines.
- `decoder.Decode` is `json.Decoder.Decode`, not `json.Unmarshal`. The
  AST check in `TestParserStreamingPathDoesNotCallJSONUnmarshal` does
  not fire.
- Returns exactly the shape the loader already expects.

**Cons**

- `json.Decoder.Decode(&value any)` internally uses the same value
  decoder as `json.Unmarshal`. The current AST test catches the literal
  identifier `Unmarshal` only, but the spirit of the test is "don't
  allocate the whole subtree at once." Option B violates the spirit
  while passing the letter.
- For schema-known attributes whose values are genuinely large (a 5 MB
  inline `aws_iam_policy_document.json` body, a 10 MB
  `aws_lambda_function.code` chunk), Option B allocates the whole blob.
  The schema-known allowlist is the only thing keeping this bounded,
  and the allowlist is not size-aware.
- Per-leaf classification becomes a post-decode walk. The walker has to
  re-traverse the decoded structure to call `Classify` on each scalar
  leaf, and substitute redacted values back into the tree. That is more
  code than the streaming walker, not less.
- If we later want to truncate or sample multi-element repeated blocks
  inline (extending PR #198), Option B has to truncate after a full
  decode. Option A truncates as it walks.

### Option C — Two-pass parse: scalars first, then schema-known composites

The first pass uses the existing streaming parser to emit all scalar
attributes and resource identities. The second pass re-reads the state
file and decodes only the schema-known composite subtrees.

**Pros**

- Streaming path stays pure. No new walker logic in the hot path.
- Composite decoding is isolated; if it ever needs more aggressive
  bounding (sampling, size limits, redaction policies), the second-pass
  code is the only thing to touch.

**Cons**

- Doubles the I/O on every parse. For a 100 MB state file, that's an
  extra 100 MB of disk reads per generation per intent.
- The collector reads state through an `io.Reader` backed by S3,
  Postgres, or a local file. The second pass needs to re-open the
  source, which is non-trivial for the S3 path and breaks the existing
  `tfstateruntime.parseCandidate` contract.
- Two-pass parsing adds a new failure mode: scalar emission succeeded
  but composite re-read failed. The reducer would see partial attribute
  data and emit partial drift candidates. Recovery semantics get
  complicated.
- The "extra walker complexity" Option A pays for is smaller than the
  "extra I/O + re-open + recovery semantics" Option C pays for.

## Decision Outcome

Eshu adopts **Option A** — streaming nested walker for SchemaKnown paths
only.

The decisive arguments:

1. Option A preserves both the letter and the spirit of
   `TestParserStreamingPathDoesNotCallJSONUnmarshal`. Option B passes the
   letter only.
2. Option A's memory growth is bounded by schema depth times average
   composite size (typically <1 KB per composite). Option B's is bounded
   only by the schema-known allowlist size, which has no upper bound on
   any individual value.
3. Option A's per-leaf classification flows naturally; the walker calls
   `Classify` as it descends. Option B requires a post-decode walk that
   duplicates the same traversal.
4. Option C's I/O cost and recovery-semantic cost both exceed Option A's
   complexity cost.

### Where The Code Lives

| Surface | File | What changes |
| --- | --- | --- |
| Composite reader | `attributes.go` | New `readCompositeValue(decoder, ...)` walks an opened `[` or `{` and returns the nested `map[string]any` / `[]any` shape. |
| Attribute reader | `attributes.go` (`readAttributeValues`) | When the resolver says `HasAttribute(resourceType, key)` for a `[` or `{` opener, call the new composite reader instead of `skipNested`. The non-known branch keeps `skipNested`. |
| Classifier seam | `attributes.go` (`classifyAttribute`) | When a preserved composite contains a leaf whose own source path is sensitive (`isSensitiveSource` returns true on `resources.<addr>.attributes.<key>.<nested...>`), the walker emits a `redact.Scalar`-stamped value at that leaf instead of the raw value. |
| Memory invariant | `parser_memory_test.go` | New assertion: peak heap growth on a 20k-instance state whose every resource carries a `server_side_encryption_configuration` block stays under `maxStreamResourcePeakHeapGrowth` (48 MB). |
| New regression | `attributes_test.go` | Positive (SchemaKnown composite captured), negative (SchemaUnknown composite stays nil/dropped), ambiguous (multi-element repeated block under SchemaKnown — first-wins truncation interaction with PR #198). |

The `readAttributeValues` signature does not change. The
`stateParser.options.SchemaResolver` (already wired in commit `ed4903f`)
becomes the gate. The parser reads composite values only when the
resolver answers yes for the `(resourceType, attributeKey)` pair AND the
caller has provided a non-empty `RuleSet.version` (so the redact policy
actually evaluates to `ActionPreserve` rather than the fail-closed
branch).

### Edge Cases This ADR Commits To Handling

Per `CLAUDE.md` §"Correlation Truth Gates" — positive, negative, and
ambiguous cases.

| Case | Class | Expected behavior |
| --- | --- | --- |
| `aws_s3_bucket.server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm` present in state JSON | Positive | Value `"aws:kms"` captured; loader flattens to dot-path; bucket E fires when config disagrees. |
| `aws_s3_bucket.versioning.enabled = false` (singleton repeated block) | Positive | Singleton array unwraps to single object, `enabled = false` captured. |
| `aws_security_group.ingress[0..N]` (multi-element repeated block, SchemaKnown) | Ambiguous | Array preserved with all elements; PR #198's first-wins truncation still fires in the loader's flattener; truncation telemetry from #198 continues unchanged. |
| `aws_iam_policy_document.statement` with provider-specific opaque blob inside | Positive | Walker captures the nested map and array shape; opaque scalar leaves get `Classify`'d like any other. |
| A SchemaUnknown composite (`server_side_encryption_configuration` on a hypothetical custom provider not in the bundle) | Negative | `HasAttribute` returns false; existing `skipNested` path runs; value stays nil; existing fail-closed behavior preserved. Memory budget unchanged. |
| Nested sensitive key inside a SchemaKnown composite (`aws_iam_user.login_profile.password`) | Positive (security) | Walker calls `Classify` on the leaf; `isSensitiveSource` matches `password` segment; leaf becomes a `redact.Scalar`-stamped value. |
| Empty array `[]` for a repeated SchemaKnown block | Ambiguous | Walker returns `[]any{}` (empty slice). Loader's flattener sees no leaves to extract; no false-positive drift candidates. |
| Deeply nested schema-known composite (5+ levels) | Positive | Walker recurses; bounded by schema depth (max 10 in current AWS schema bundle). Stack-safe (Go's default stack grows). |
| State JSON shape disagrees with schema (provider released a new minor version) | Negative | Walker reads whatever JSON is there; flattener handles unknown nested keys; no parser-side schema validation. The schema is for trust, not for shape enforcement. |
| 100 MB state file with one schema-known composite per resource | Positive | Per-resource composite is small; total additional heap growth bounded by `20k instances × ~1 KB per composite ≈ 20 MB`. Under the 48 MB ceiling. |
| A SchemaKnown composite whose value is genuinely large (10 MB inline JSON policy) | Positive (but flagged) | Walker reads it all. This is the one Option A case that allocates a large blob. Mitigation: a follow-up issue tracks size-aware truncation. Today the AWS schema bundle does not include any nested block that holds multi-MB scalar leaves; the regression test asserts this with a synthetic 10k-character scalar inside an SSE block. |

### Memory Proof Required Before Acceptance

Per `CLAUDE.md` §"Performance Workflow", this ADR commits the
implementation PR to producing before/after evidence:

- **Fixture**: a synthetic 20k-instance state where every instance
  carries a populated `server_side_encryption_configuration` composite.
  Built from `largeResourceInstancesStateReader` in
  `parser_memory_test.go:367` (extended to inject the composite).
- **Metric**: `runtime.MemStats.HeapAlloc` peak via
  `measurePeakHeapGrowth` (existing helper at
  `parser_memory_test.go:312`).
- **Acceptance**: the new composite-capture path must keep peak heap
  growth under `maxStreamResourcePeakHeapGrowth = 48 MB` on the synthetic
  fixture. If it exceeds, the implementation must STOP and re-design.
- **Also measured**: parse wall time on the same fixture (before and
  after), allocation count via `b.ReportAllocs()` in
  `parser_bench_test.go`. Material regression (>10%) in either metric
  requires explicit ADR addendum.

## Consequences

### Positive

- Bucket E (`attribute_drift`) fires end-to-end on the Tier-2 verifier.
  Tier-2 ships with four of six buckets working; buckets C and F (which
  need two collector generations of the same state) move to the v2.5
  follow-up.
- The redact API's `ActionPreserve` for composites becomes a real
  promise: the parser captures the value, the loader flattens it, the
  classifier compares it.
- Per-leaf sensitive-key matching now works on nested composites. A
  `password` key inside `aws_iam_user.login_profile` gets redacted at
  the leaf, not by dropping the whole composite.
- The fix is bounded to `terraformstate/attributes.go` and a new
  regression in `parser_memory_test.go`. No collector orchestration,
  reducer, or storage change.

### Negative

- Parser code grows. `attributes.go` adds the composite walker; the
  package stays well under the 500-line ceiling but the file size
  approaches it (this is the next file to split if Scope-3 or follow-up
  bug fixes add more behavior).
- The streaming-path memory invariant gets a new lower bound on the
  budget. SchemaKnown composite capture adds measurable heap growth on
  every state file, even small ones. Operators with very large state
  files (>500 MB) that contain many schema-known composites should
  expect a measurable bump in collector memory pressure.
- New code path means new failure modes. A corrupt JSON nested block
  could now produce a parser error on a path that previously succeeded
  via `skipNested`. The walker must surface "I tried to read this
  schema-known composite and failed" as a `parse_warning` fact, not as
  a fatal collector error.

### Risks

- **Schema drift between bundle and reality.** AWS releases a new
  provider version that adds a nested block the bundle doesn't know
  about. The walker's `HasAttribute` returns false; the composite gets
  skipped; bucket E silently regresses for that attribute. Mitigation:
  the `unresolved_module_calls_total` precedent in PR #169 suggests a
  similar `eshu_dp_drift_schema_unknown_composite_total{resource_type,attribute_key}`
  counter; the implementation PR includes it.
- **Sensitive-key leakage through nested composites.** A composite is
  schema-known but a nested leaf is named `password` or `secret`. The
  walker must call `Classify` per leaf, not once per composite. The
  regression test set includes a positive case for this.
- **Multi-element repeated blocks interact with PR #198 truncation.**
  PR #198 truncates at the first element in the loader's flattener. The
  walker emits the full array; the truncation still happens downstream.
  The regression test asserts the truncation log still fires.
- **Backward compatibility with already-emitted facts.** Generations
  emitted before this ADR's implementation will not have the composite
  values. The drift handler must not regress on those generations.
  Mitigation: the existing fail-closed nil-value behavior already does
  the right thing (no drift fires when the value is absent), so prior
  generations stay silent rather than producing wrong drift candidates.

## Resolved Questions

Resolved 2026-05-12 by the owner. Each entry records the resolution and
the one-line rationale.

1. **On malformed JSON inside a schema-known composite: emit a
   `parse_warning` fact, or fall back silently to `skipNested`?**
   **Resolution:** neither — use `slog.Warn` plus a counter increment;
   do not introduce a new fact kind. Modified from the initial
   recommendation; rationale: avoids new fact-kind contract surface in
   a PR already four bugs deep. The counter (per Q2) carries the
   "how often" signal and the structured log carries the diagnostic
   detail (source path, `resource_type`, `attribute_key`, JSON-decode
   error). That gives operators the same visibility without adding a
   new contract. `CLAUDE.md` §"Observability Contract" names
   slog + counter as the standard pair for "new retry/skip path."
   Promotion to a fact kind is a follow-up if dogfood evidence shows
   operators need durable history of composite-capture failures.

2. **Should `eshu_dp_drift_schema_unknown_composite_total` carry both
   `resource_type` and `attribute_key`, or only `resource_type`?**
   **Resolution:** counter with `resource_type` label only;
   `attribute_key` stays in the structured log. Rationale:
   `CLAUDE.md` §"Observability Contract" forbids high-cardinality
   values in metric labels. Resource-type cardinality is bounded by
   the schema bundle; attribute-key cardinality is not.

3. **Include size-aware truncation for genuinely large
   schema-known composites (10+ MB) in v1?** **Resolution:** no.
   Rationale: the synthetic 10k-character regression test bounds the
   worst case; the current AWS schema bundle has no nested block with
   multi-MB scalar leaves. File a follow-up stub if dogfood surfaces
   multi-MB composites.

4. **Does the v2.5 follow-up (buckets C and F) inherit this ADR's
   walker, or use a different approach?** **Resolution:** inherit.
   Rationale: buckets C and F are an orchestration problem (drive the
   collector twice with state/repo swap), not a parser problem. The
   walker applies the same way under the second pass.

5. **Does the implementation need to update PR #198's truncation
   contract?** **Resolution:** no. Rationale: the walker (parser
   layer) and the flattener (loader layer) operate at different
   layers. The regression test commits to: multi-element repeated
   block on a SchemaKnown path causes PR #198's truncation log to
   fire with `multi_element.source="state_flatten"`. That is the gate.
   No contract change.

## References

- HashiCorp Terraform state file format reference (the
  nested-singleton-array shape this ADR commits to capturing):
  <https://developer.hashicorp.com/terraform/internals/json-format>
- HashiCorp AWS provider 5.100.0 schema (the bundled schema this ADR
  reads via `terraformschema.EmbeddedSchemasFS()`):
  <https://registry.terraform.io/providers/hashicorp/aws/5.100.0/docs>
- Issue #187 — Tier-2 tfstate drift E2E proof.
- PR #198 — multi-element repeated-block first-wins truncation in the
  loader's flattener.
- Commit `ff4c9ed` on branch `feature/tier-2-tfstate-drift-e2e-187` —
  Scope 2 redaction-rules wiring.
- Commit `ed4903f` on branch `feature/tier-2-tfstate-drift-e2e-187` —
  Scope 3 provider-schema trust through attribute classification.
- `redact.RuleSet.Classify` reference: `go/internal/redact/policy.go:112`.
