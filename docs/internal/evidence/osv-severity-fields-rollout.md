# OSV severity fields: shared golden, publication proof, rollout, and readback plan

Item 1.1 of the advisory-intelligence epic (#9209, issue #9692, defect D1).
This note records what shipped, how it was proved, what a deploy changes, and how
to read the result back in the QA environment. It carries no code. The shared
golden and the in-tree collector it describes were merged in eshu#9750. The
design context is in
[the advisory-intelligence phases](../design/advisory-intelligence-phases.md).

## What changed

The OSV `vulnerability.cve` fact now carries `cvss_vector` and `severity_label`,
the same field names and types the NVD fact already used. Before, the OSV
envelope wrote only a raw `severity` array and `database_specific`, so the
reducer, which decodes `cvss_score`, `cvss_vector` and `severity_label`, saw
nothing for most OSV findings.

- `cvss_vector` is the first entry of the highest CVSS type in the record's
  top-level `severity[]` whose score starts with that type's prefix: CVSS_V4
  (prefix `CVSS:4.0/`), then CVSS_V3 (prefix `CVSS:3.`). CVSS_V2 and unknown
  types never fill it, because a v2 vector has no prefix and the reducer's score
  calculator (`go/internal/reducer/supplychain/core/cvss`) reads v3.0, v3.1, and
  v4.0 only.
- The collector's selection is prefix-only. It does not validate the vector and
  does not rank v3 minor versions: the first `CVSS:3.`-prefixed entry wins,
  whether 3.0 or 3.1. A malformed V4 value with the right prefix is therefore
  selected and hides a valid V3 entry (a prefix-only string such as `CVSS:3.1/`
  also passes). The validation boundary is the reducer: `cvss.BaseScore`
  validates the vector, and a malformed, unsupported, or zero-scoring vector
  yields no score, so the label sets the bucket. A populated `cvss_vector` is
  not proof of usable severity; read the reducer's `derived_from_vector` and
  `label_only` decisions for that.
- `severity_label` is `database_specific.severity`, trimmed and lower-cased, with
  `MODERATE` read as `medium`; the vocabulary is critical, high, medium, low. An
  unrecognized, missing, or non-string value leaves it empty.
- No `cvss_score` is written. The reducer derives severity from the vector or
  the label (item 1.2, #9701: `DeriveSeverity`; the bucket SQL falls back to the
  selected label) and marks a derived score as derived. That derivation is
  already on `main`, so OSV findings can leave severity `none` as soon as their
  scopes carry the new fields and `supply_chain_impact` runs again for them.
- Only the top-level fields are read; an `affected[]` severity or label is
  ignored. A withdrawn advisory gets the same two fields as any other.

## The shared golden

One file defines the rule for both collectors:
`sdk/go/factschema/fixturepack/payloads/osv_severity_golden.json`.

| Property | Value |
| --- | --- |
| SHA-256 | `9e7b53ee9e490c3d71130762f3f73beae2f586aecff87e86d9003e97eff54fee` |
| Cases | 12: V3 with a high label, V4 over V3 with `MODERATE`, first V3 wins, V2 only, V3 beats V2, a V4 without the vector prefix falling back to V3, an unknown type and label, a non-string label, no severity, mixed case and whitespace, V4 only, and an empty `severity: []` (echoed as null) |
| Owner | the factschema fixture pack: the canonical copy and the in-tree collector were merged in eshu#9750; the eshu-sdk port is published with each factschema tag |
| Readers | the in-tree collector (reads the canonical file by path), and the extracted collector (reads `fixturepack.OSVSeverityGolden()` from the published module; merged as its #35) |

The fixture pack's own test derives every expected field from the case's record
and requires every rule shape to stay represented, so the golden cannot drift
into an internally consistent but wrong state. The digest is pinned in three
tests: the canonical fixture pack, the eshu-sdk port, and the extracted
collector, which also asserts the 12 cases. Changing the golden therefore means
a reviewed change to the canonical file and the port, a new factschema release,
and a pin bump with a new digest in the extracted collector.

## Publication proof (factschema v0.1.0-rc.11)

The golden shipped in eshu-sdk PR #64 and was released by the release workflow,
not by hand.

| Fact | Value |
| --- | --- |
| Release run | 41231620015: verify, tag, and prove all succeeded |
| Tag | `factschema/v0.1.0-rc.11`, lightweight, at commit `d966966bbb` |
| Proxy zip sum | `h1:8psQF77P4XAnBXBKV5QcUVlPqOt5AOTEzLcSdgN9w+I=` |
| Proxy go.mod sum | `h1:GysO1GSvcUYSSl7ydDx34MYRSw4yI2DgADT0twajVXY=` |
| Proxy origin commit | `d966966bbb`, equal to the verified commit |
| Golden from the proxy copy | SHA-256 `9e7b53ee…fee`, reproduced from the module the proxy served |

A later factschema release, rc.12 (run 41250267912, tag at `7bfa786892`), is not
part of this change. The extracted collector pins rc.11 and its go.sum holds the
two sums above. At the base of the core change, `main` pins rc.10, which the
core change keeps because it reads the canonical golden by path and its helper
test uses no SDK API.

## Rollout effect

- Every OSV `vulnerability.cve` payload gains the two keys. They are empty
  strings when the record has no severity, because the golden requires them.
- The payload content hint (`freshnessHint`) therefore changes once for every OSV
  record, while stable keys, source refs, and the schema version do not change.
  For one example input the extracted collector's hint moves from `2e3cf1eab238…`
  to `95f656589ab6…`; the input and a command that prints both are in the
  extracted collector's PR body. Repeated runs at one commit give identical hints.
- Each OSV scope re-emits once after the new collector rolls out. That
  re-emission is also how stored facts get the fields: until a scope is
  collected again, its stored facts carry neither field.
- Derived OSV targets rotate through hash slots, one slot per reconcile window.
  P is the smallest power of two with P times the target limit at least twice
  the target count, which bounds the mean slot size, not the largest slot. Every
  target is replanned within P windows only if P stays stable (the target count
  does not cross a power-of-two multiple of half the target limit), P
  consecutive windows run and succeed, and no slot exhausts the read or planning
  limits. A slot that does is cut short on every rotation, and the targets past
  the limit are never planned in it.
- Before declaring regeneration complete, check the coordinator's log line
  "workflow coordinator read owned package target window" for `truncated=false`
  on every window, and the run's skip evidence for any
  `derived_target_budget_exhausted` entry (it names the unread count). Either
  one means some targets were not replanned. The deployed window length and the
  wall-clock time to full regeneration have not been measured.

## Release gate state

No satellite tag exists after `v0.1.2`, and none of these changes bumps a
manifest version or changes an infrastructure image digest pin. Cutting a
satellite release remains a separate decision.

## QA readback plan

The baseline: on 2026-10-31 at about 20:26Z the coordinator session read, with
no mutation, `GET /api/v0/supply-chain/impact/inventory?group_by=severity&impact_status=affected_exact`
inside the QA API service. 9,980 of 10,017 `affected_exact` findings had
severity `none`. That read was not re-measured by a reviewer. It ran against the
core build `bdf2823363`, which already contains the item 1.2 derivation (#9701),
so derivation was live, and it ran before the extracted collector change (#35)
had run in the QA environment. An earlier figure from a different method and population is
not comparable; do not read the two as a trend.

What to expect: the QA environment already runs the reducer that derives
severity, so the extracted collector change is the missing input. Stored OSV
facts carry neither field until their scope is collected again. Once the new
collector has run, the OSV scopes are collected again, and `supply_chain_impact`
reruns for each scope, `affected_exact` findings whose OSV record carries a
vector or label should leave severity `none`, and the `none` count should fall.
The merged vulnerability-impact reference describes the same convergence under
"Severity convergence".

Run the readback inside one hour of the satellite rollout. Use the same
read-only queries each time and record the time of each read:

1. The inventory query above. Pass: the `none` count is lower than 9,980, with
   the denominator (about 10,017) read in the same call. Record both numbers.
2. The rate of `eshu_dp_supply_chain_severity_decisions_total` by `outcome`
   (`derived_from_vector` and `label_only` should rise), and findings counted by
   `provenance.selected_severity_score_source` on the active generation.
3. A sample of OSV `vulnerability.cve` facts: both keys present. Expect a
   non-empty `severity_label` only where `database_specific.severity` is a
   recognized string (critical, high, medium, moderate, or low, any case, with
   surrounding space ignored); `moderate` and `medium` both give `medium`.
   Expect an empty label where the value is missing, an unrecognized string, or
   not a string: the collector leaves those empty on purpose, so an empty label
   there is correct output, not a failure. The vector count can be lower than
   the label count, because CVSS_V2 entries do not fill it.

If nothing moves inside the hour, that is a finding to diagnose, not a reason to
wait: check that the collector instance is running the new build, that OSV
scopes are being collected (their source-state rows), and that
`supply_chain_impact` ran after the new facts landed. A scope that does not
rotate is re-driven as described in "Severity convergence". A `none` share that
stays high for a source that publishes severity points at the collector.

NOT_CHECKED: the readback itself, the rotation time and the deployed collection
interval, and the effect on any other severity source.
