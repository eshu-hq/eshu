# Replay tier live-test enrollment (#7533)

The blocking replay-tier job owns 23 existing untagged
`go/internal/replay/offlinetier/delta_tier_*_live_test.go` files. They declare
27 top-level tests. Two declarations are hermetic: the existing
`TestEntityRetractManifestBinding` and the newly selected
`TestGovScopeMaterializationEntitySourcesHaveFiles`. The other 25 require the
real pinned NornicDB fixture. No Go source, test body, cassette, graph query,
backend setting, or runtime profile changed in this enrollment.

The immutable source map in `scripts/lib/replay_tier_inventory.json` binds each
file's SHA-256 digest and declared tests. The ledger validator requires all
23 rows to remain `dedicated_ci`, `runner: replay-tier`, `backends: nornicdb`.
The runner's first Go invocation selects all 27 declarations plus its existing
R-5 projection and provenance checks. The separate query invocation keeps all
three SQL-table blast-radius checks. Both commands retain their package order,
`-p=1`, `-count=1`, and the same timeout and fixture configuration.

The runner checks a clean Git checkout before container startup, before each
Go invocation, and after each event check. It rejects shell or effective Go
`GOFLAGS` and `GOWORK` overrides, including an ignored `go.work`, so compiled
code stays bound to the reviewed commit.

Both invocations emit Go JSON events to files distinct from stderr and copy
those raw streams to runner stdout before temporary cleanup. The
verifier requires one ordered run/pass pair for every selected top-level test
in its expected package, a package pass, and a zero direct Go exit. It rejects
selected skips and failures, including child skips and failures. It renders Go
`Output` events only after the structured check passes, so the six previous
PASS-line checks still run on their actual output. The workflow, registry, and
pre-PR selector route changes to the inventory, validator, ledger fragments,
runner, and mirrors to the same blocking job. The independent ledger
inventory also checks that queue selection, checkout, Go setup, contract
verification, and backend execution remain active in that job with the
existing merge-group selection condition and direct proof commands.

The unchanged complete runner passed on baseline
`e0e7f03f9763b25e7f5b1163e2bdafb4f5100f24` in 607.443545 seconds
(10m 7.44s), with 26 then-selected candidate test passes, all six old
non-vacuity checks, and 33 top-level passes across its two invocations. The
added governance check was not selected in that baseline. This is a measured
verification cost, not a speedup or a general NornicDB support claim.

The full changed runner on candidate `42dc57c8325b6fd8e8a859555e56711676230bfd`
exited 0 in 245.684093 seconds (4m 5.68s). Its first invocation passed 31
selected top-level tests, including all 27 enrolled declarations (25 live and
two hermetic); the separate blast-radius invocation passed all three selected
top-level tests. The reviewed-source guard passed, the disposable fixture was
clean, and the owned backend container was removed after the run. This is a
candidate verification result; the workflow and hosted reviewed-checkout gate
are still NOT_CHECKED. The baseline above selects a different test set, so
these durations are not a speedup comparison. The retained-state NornicDB
conformance investigation is separate and remains held. #7533 remains open
for the other legacy rows.

No-Observability-Change: this CI-only change adds no production metric, span,
log key, status, worker, or graph write. Its operator signal is the existing
blocking replay job plus the stricter test and package event failures.
