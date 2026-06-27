# Evidence: Chef evidence-kind → evidence_type mapping (rc-33)

Scope: `go/internal/reducer/cross_repo_evidence_type.go` gains one entry mapping
the new `CHEF_COOKBOOK_DEPENDENCY` evidence kind to its lowercase
`chef_cookbook_dependency` admission-audit `evidence_type` label. The CI hot-path
gate flags any change under `go/internal/reducer/` by location, so this file
records the required evidence.

## What changed

A single key→value entry in the static `evidenceKindToType` string map. No
Cypher, no graph writes, no worker/lease/batch/concurrency logic, no query shape,
no runtime/Compose/Helm setting. The map is consulted only to attach a
human-readable `evidence_type` string to an admission-audit record; an unmapped
kind already degrades gracefully to the raw kind string, so the entry is a
labeling refinement, not a behavioral change.

No-Regression Evidence: the change is one constant string-map entry on a
non-Cypher, non-concurrency code path; it adds no traversal, allocation, lock,
or query work. Baseline and after are identical for every existing evidence kind
(the map is keyed; existing lookups are unaffected). Backend-neutral. The full
B-7 golden-corpus gate runs green with this change (rc-33 = 1, 46 pass /
0 required-fail), confirming no projection or admission regression.

No-Observability-Change: no metrics, spans, or logs are added or altered. The
mapped `evidence_type` string flows into the existing admission-audit record
field that already carried every other evidence kind's label; no new operator
surface is introduced.
