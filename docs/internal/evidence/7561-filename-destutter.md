# 7561 — filename destutter: reducer/incident, reducer/crossplane

Issue: #7561. Branch `refactor/7561-incident-crossplane-destutter`, base `origin/main`
306eac09c893.

Seventeen files whose stem repeated their own leaf directory were renamed, dropping
the repeated word. This note exists because ten of the renamed files carry hot
reducer content (Cypher builders, writers, materialization handlers) that
`scripts/verify-performance-evidence.sh` selects on, so the gate requires tracked
evidence in the repo rather than only in the PR body.

## What changed

- `go/internal/reducer/incident/`: the `incident_` prefix dropped from twelve files
  (seven production, five test).
- `go/internal/reducer/crossplane/`: the `crossplane_` prefix dropped from five files
  (three production, two test).
- Reference updates only: three design docs, `telemetry-coverage.md`,
  `crossplane.md`, two reference docs, the projector satisfaction README, Go comments
  in `payloadusage`, `goldengate`, `storage/cypher`, both costcounting tests, the
  reducer-root registry and idempotency exemption strings, `payloadcore` README, both
  scoped `AGENTS.md`/`README.md` pairs, and three removed LINE rows in
  `scripts/docs-citations-baseline.txt` (citations converted to file anchors, which the
  doc-citations gate tracks as debt decrease).

## Why no measurement is reported

No-Regression Evidence: none of the seventeen renames changed a byte of any file.
`git diff --find-renames --numstat origin/main...HEAD` reports `0 0` for all seventeen,
and `git diff --find-renames --name-status` classifies all seventeen `R100`. The
remaining modified files add and delete a handful of lines, none of them an executable
Go statement: they are Markdown pointers, Go comments, test-exemption strings, and
three baseline deletions.

There is therefore no before/after measurement to report, and inventing one would be
dishonest. The claim this note makes is narrower and checkable: the compiled behaviour
of both packages is unchanged because their source bytes are unchanged. The proof is:

- `go build`, `go vet` and `go test -count=1` over `./internal/reducer/incident/...`
  `./internal/reducer/crossplane/...` plus the comment-touched `reducer` root,
  `payloadusage`, `goldengate`, `costcounting`, and `projector/crossplane` trees all
  exit 0.
- `go test -list '.*'` over the two renamed trees yields 30 test names on the base
  commit and the same 30 on this head. The sorted sets diff empty. A control that
  drops one name from the after-list makes the same comparison report a difference,
  so the empty diff is a real pass and not a broken command.

No Cypher, graph write, queue, lease, batching, reducer projection or materialization
path is touched. No backend, corpus, profile, topology or storage state is involved,
so there is no baseline manifest to compare against and no queue or row count to
report.

## Observability

No-Observability-Change: no metric, span, log key, status field or pprof surface is
added, removed or renamed. All seventeen files keep their contents byte for byte, so
every instrument the handlers register keeps its name and labels. Operators see no
difference; nothing in a dashboard, alert or runbook needs updating.
