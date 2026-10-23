# Delta union scheduled enrollment (#8785)

The existing `go/cmd/projector/delta_union_live_test.go` contains two exact
proofs. The daily, manual, and scoped PR workflow runs them on an isolated
PostgreSQL 18.6 service and Neo4j service. Its PostgreSQL database is disposable:
the shared overlay harness drops the public schema. Its Neo4j graph is likewise
owned by the job, because the harness clears it. The job uses pinned service
images, an anchored Go selector, `-race -count=1`, a twelve minute test timeout,
`pipefail`, strict Go JSON event accounting, and uploaded events. The workflow
sets nonempty Neo4j username and password values even with `NEO4J_AUTH: none`.

The static mirror compares the actual source functions with the selector and
rejects disabled jobs or steps, suppressed failures, changed service pins,
missing routes, and missing event verification. The ledger pins this file alone
to `delta-union-scheduled` and `backends: neo4j`. The five tests in
`superseded_delta_overlay_live_test.go` remain scheduled.

The local baseline is `/tmp/eshu-7533-drive/8785-local/manifest.txt`, with the
second race run in `go-test-attempt2.json`; it belongs to the pre-enrollment
source and harness and is not a hosted workflow result. The hosted PR check is
required for acceptance of this enrollment. Deployed QA is NOT_CHECKED; the
issue remains open for that evidence.
