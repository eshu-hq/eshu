# W1f SBOM, CI, Work Item, Incident, And Security Alert Contract Evidence

Issue #4792 migrates W1f live collector emit paths for SBOM, attestation,
scanner-worker SBOM generation, CI/CD runs, security alerts, Jira work items,
PagerDuty incidents, and incident routing from helper or inline payload maps to
canonical `factschema.Encode*` payload construction. The change does not alter
collector scheduling, claims, leases, retries, graph writes, reducer queue
behavior, provider API calls, persisted fact kinds, or runtime deployment shape.

Benchmark Evidence: on go1.26.4 darwin/arm64 Apple M5 Max, representative W1f
constructor benchmarks compared the `origin/main` JSON round-trip encoder path
against this branch's direct-map typed factschema emitters on the same inputs:
SBOM document baseline 3652-4431 ns/op, 2667 B/op, 50 allocs/op; branch
454.0-531.6 ns/op, 1560 B/op, 12 allocs/op. CI run baseline 5570-6769 ns/op,
4438 B/op, 89 allocs/op; branch 663.7-811.7 ns/op, 1640 B/op, 19 allocs/op.
Work item record baseline 6340-7262 ns/op, 4542 B/op, 92 allocs/op; branch
1002-1180 ns/op, 1804 B/op, 28 allocs/op. Incident record baseline
5703-5965 ns/op, 4382 B/op, 109 allocs/op; branch 1133-1168 ns/op, 2832 B/op,
36 allocs/op. Incident routing applied alert route baseline 5595-5918 ns/op,
4727 B/op, 91 allocs/op; branch 562.6-590.3 ns/op, 1648 B/op, 9 allocs/op.
Security alert repository alert baseline 9086-11785 ns/op, 7058-7059 B/op,
158 allocs/op; branch 1102-1182 ns/op, 3104 B/op, 27 allocs/op. The measured
command was `GOCACHE=<worktree>/.gocache go test ./... -run '^$' -bench
'^BenchmarkW1fEncodeNoRegression$' -benchmem -count=5` from
`sdk/go/factschema`; the baseline worktree was detached at `origin/main` and
carried only the benchmark file.

No-Regression Evidence: focused TDD proof first failed before the encoder
adoption (`TestContractEncodeAdoptionRatchet`,
`TestAdoptedEncodePathsUseDirectMaps`, and
`TestW1fTypedFamiliesCarryPayloadSchemas`), then passed after the typed emitters,
direct-map encoders, and registry schema rows landed. Post-rebase focused gates
passed for the touched collector packages, `go/internal/facts`, and
`sdk/go/factschema`; `make pre-pr` remains the final local pre-push gate for
this branch.

No-Observability-Change: W1f #4792 changes only in-process payload construction
before the existing fact envelope commit path. It adds no worker, queue, lease,
retry boundary, graph write, runtime knob, route, metric instrument, metric
label, span, or log key. Operators continue to diagnose these collector paths
through the existing collector observe/commit spans, fact-count and duration
metrics, structured commit logs, status surfaces, and downstream reducer
dead-letter classification for malformed payloads.
