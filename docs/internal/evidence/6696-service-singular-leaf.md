# #6696 AWS services-to-service rename: Phase-1 no-regression evidence

Phase-1 record: this note captures the `services/` to `service/`
singular rename as it stood when that phase landed. Later phases in
this PR moved the tree again (`go/internal/collector/awscloud` to
`go/internal/collector/cloud/aws`, with de-stuttered identifiers and
filenames), so translate the `awscloud/...` paths below to their
`cloud/aws/...` successors when re-running anything here. The PR
body carries the current end-state proof; this file is the
phase-1 receipt, not a re-runnable guide at this head.

In-tree prep only: `go/internal/collector/awscloud/services/*` (134
service packages) moves to `go/internal/collector/awscloud/service/*`
via a single parent-directory `git mv`. Package names are unchanged
(leaf package clauses already match their directory names); only
import paths change. All `collector/awscloud/services/` references
repoint mechanically to `collector/awscloud/service/` across Go
imports (582 files), moved leaf trios, and live docs. No repo
cutover (cutover follows #4047/#6707, not this issue). GCP/Azure
untouched.

Out of scope on purpose: three dated evidence notes
(`5717-ami-node-class.md`, `6695-access-posture-leaf.md`,
`6834-code-divergence-theory.md`) record paths as they stood when
written — repointing would falsify those records, so they keep the
old paths. The CHANGELOG entry for the shipped #5717 AMI work keeps its
as-shipped path for the same reason. The golden snapshot prose (`e2e-20repo-snapshot.json`)
likewise narrates historical proofs and is untouched. The
moved-file-refs gate passes vacuously (every vacated file pairs to
its rename; nothing dangling). `test-verify-performance-evidence.sh`
builds a synthetic tree whose `services/s3` path is arbitrary
fixture content, not a repo pointer — untouched.

Two raw `go/internal` LINE citations caught new `service/` paths
with line numbers; per gate prescription both de-number to file
anchors and their two burn-down rows drop from
`docs-citations-baseline.txt` (1:1, asserted counts):
`1146-ec2-instance-node.md` (`scanner.go:13-17` range) and the
postgres drift-completeness test (`client.go:89-94` range).

The parent-dir rename defeats default rename detection, so the
telemetry gate's added-file diff misread all 2,838 moved files as
new stages and demanded coverage rows for files that only changed
address. Root-cause fix in `scripts/verify-telemetry-coverage.sh`:
pair renames explicitly (`-M`) with a raised rename limit
(`diff.renameLimit=20000`); with pairing, all 2,838 files resolve
as R-paired moves and the new-stage set is empty. No coverage rows
added, so `telemetry-coverage.md` stays at its 1106-line pin.

`servicekind_guard_test.go` reads the services directory at runtime
via `filepath.Join(dir, "services")`; repointed to `"service"` with
its helper renamed to `serviceSourceDir` and comments updated.
`extractor.go` layout comments updated. Verified the expected Encode
set is unchanged (no emitter logic touched).

## No-Regression Evidence (#6696):

- Baseline: `origin/main` at `b89e20ae6` (post-#6905 merge).
- After: branch `feat/6696-service-singular` (this leaf).
- Backend/version: no live backend exercised. Unit tests only (live
  AWS tests gate behind credentials and skip). Toolchain
  `go1.27.1 darwin/arm64`, same machine.
- Build: `go build ./...` clean. `go vet
  ./internal/collector/awscloud/...` clean.
- After measurement: `go test -count=1
  ./internal/collector/awscloud/ ./internal/collector/awscloud/service/...`
  exit 0 — 403 packages ok, zero FAIL. Sample: `awscloud` 0.463s,
  `service/ec2` 3.137s, `service/s3` 3.135s.
- Terminal counts: all test-bearing touched packages green, zero
  failures.
- Query/concurrency proof: not applicable — no query, graph, queue,
  or goroutine surface touched. Added-line scan: import-path
  repoints, two comment updates, one test-helper rename; no Cypher,
  SQL, metric, or error-path changes.
- Telemetry/log/status evidence: no metric, span, log, or audit
  emission touched; no new files over the 500-line caps
  (`verify-markdown-line-cap --all` shows only shrink NOTEs).
- Gates: `verify-moved-file-refs --base origin/main` clean (no
  vacated paths); `verify-doc-citations.sh` OK (257 test, 274
  fixture, 449 LINE occurrences); `verify-dirgate` clean.
