# #5458 — repository_hosting / vulnerability_hint Disposition (Slice 1 of 3)

Issue #5458 (epic #5455) named four `package_registry` fact kinds collected
and read by nothing: `vulnerability_hint`, `registry_event`,
`repository_hosting`, `package_artifact`. This note records the disposition
for the two lightest of the four — `repository_hosting` and
`vulnerability_hint` — as **formalized disclosures**, not new consumers.
`package_artifact` and `registry_event` are separate, heavier PRs under the
same issue that add real consumers.

## Stale premise in the issue

The issue's Evidence section states these kinds are "read by nothing" and
implies the #5474 D2 consumer-existence gate
(`TestEveryRegistryKindHasConsumerOrDisclosure`,
`go/internal/mcp/kind_consumer_existence_test.go`) would fail on them. That is
no longer accurate: all four kinds were already added to the digest-pinned
disclosure ledger (`grandfatheredUnconsumedKinds` /
`kindDisclosureEntries` in `go/internal/mcp/kind_disclosure_ledger.go`)
during the #5474 signal-rebuild backfill, so all four already pass the gate.
#5458 is therefore not "wire a consumer or the gate goes red" for these two
kinds — it is a deliberate **per-kind disposition decision**: keep the
disclosure (and say why, on the record), or build a real consumer instead.
This PR sharpens the ledger `Reason` strings and the registry YAML comments
to record that decision instead of leaving a bare "no consumer" note.

## Decision: package_registry.repository_hosting — KEEP DISCLOSURE

Schema: `sdk/go/factschema/schema/package_registry.repository_hosting.v1.schema.json`
— required fields `provider`, `registry`, `repository`; optional
`upstream_id`, `upstream_url`, `repository_type`, `ecosystem`.

This payload is **registry/Artifactory feed topology**: which provider hosts
a package, under what registry/repository namespace, and (optionally) which
upstream it proxies. It does not carry a package→source-repository or
package→commit binding. Epic #5455's stated end state is "any repo walks to
its published artifacts" via graph provenance edges (PUBLISHES, BUILT_FROM,
etc.) — `repository_hosting` does not advance that backbone; it describes
hosting infrastructure, not provenance.

- No reducer decode seam, no projector read site
  (`go/internal/projector/package_registry_canonical.go:120-123` explicitly
  lists it as intentionally unhandled), no query read model.
- Round-2-verified zero real-consumer signal (2026-07-21):
  `rg -n "PackageRegistryRepositoryHostingFactKind" go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g '*.go'`
  (excluding `_test.go`) → 0 matches; same for the wire-string literal.
- **What would change this decision**: a future read surface that needs to
  answer "where is this package hosted / what upstream does it proxy"
  (e.g. surfacing registry feed URLs to a UI or API), independent of the
  provenance-backbone work.

## Decision: package_registry.vulnerability_hint — KEEP DISCLOSURE, deferred to #5462

Schema: `sdk/go/factschema/schema/package_registry.vulnerability_hint.v1.schema.json`
— required `package_id`, `advisory_id`, `advisory_source`; optional
`vulnerability_id`, `cve`-adjacent fields, `affected_range`,
`fixed_version`, severity/summary/url.

This is registry-native advisory metadata (e.g. an npm/PyPI advisory feed
hint). It is read today, but only as a **join-key filter, never decoded**:
`go/internal/storage/postgres/facts_active_supply_chain_impact.go` includes
`'package_registry.vulnerability_hint'` in its `fact.fact_kind IN (...)`
allowlist (line 46) so envelopes of this kind are fetched for a bounded
supply-chain-impact intent, and the same query's generic
`payload->>'package_id'` / `payload->>'purl'` predicates can match against
its payload — but nothing ever unmarshals the payload into a typed struct or
reads its advisory-specific fields (`advisory_id`, `advisory_source`,
`affected_range`, `fixed_version`, etc.). Verified directly against the SQL
in that file as part of this change.

Building real decode-side corroboration for this kind here would duplicate
work that belongs to the dedicated `vulnerability_intelligence` family and
epic #5462's supply-chain-impact reconciliation. Wiring a registry-native
advisory-hint consumer is therefore **explicitly deferred to #5462**, not
built as part of #5458.

- No reducer decode seam
  (`go/internal/projector/package_registry_canonical.go:120-123` lists it as
  intentionally unhandled), no query read model beyond the join-key filter
  above.
- **What would change this decision**: #5462 explicitly taking on
  registry-native advisory corroboration and deciding it belongs on the
  `package_registry` reducer path rather than the `vulnerability_intelligence`
  family.

## What this PR does NOT do

- Does not touch `package_registry.package_artifact` or
  `package_registry.registry_event` — those keep their existing disclosure
  entries unchanged; #5458's other two PRs add real consumers for them.
- Does not add a graph/query consumer for either kind covered here.
- Does not touch the golden corpus: neither kind appears in
  `testdata/cassettes/` or `testdata/golden/e2e-20repo-snapshot.json`
  (verified — zero `rg` hits), so no cassette/snapshot update is needed or
  included.

## Mechanics: keeping the digest-pinned ledger in sync

`grandfatheredUnconsumedKinds` pins `sha256(family + ":" + kind + ":"
+ reason)` per kind; `kindDisclosureEntries` is the source of truth
`disclosedKindsUnchanged` recomputes and compares against it (both
directions — extra or stale keys fail the gate). Sharpening a `Reason`
string changes its digest, so both the entry's `Reason` in
`kindDisclosureEntries` and its corresponding hex digest in
`grandfatheredUnconsumedKinds` were updated together in the same commit.
The new digests were computed via `kindDisclosureDigest` against the exact
final `Family`/`Kind`/`Reason` triples now in source (not hand-computed
against draft text), then verified green by
`TestEveryRegistryKindHasConsumerOrDisclosure` and
`TestDisclosureLedgerDigestPinned`.
