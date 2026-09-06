# Pre-PR selection evidence

No-Regression Evidence: The [runner integration tests](../../../go/cmd/ci-gates/execute_prepr_test.go)
exercise the actual CLI, mandatory whole-module commands on unselected paths,
command-result reuse, failed-result propagation, and rejection of missing or
CI-only core commands before execution. A bounded process handshake checks
that build and vet overlap the ordered formatting and lint stage. A regression
appends a failing mandatory command and verifies execution, a blocking report,
and the original failure in the reuse map when the work list grows.

The shell scheduling tests preserve FULL/FAST routing. Classifier and Git
collector regressions cover skill Markdown, executable and configuration
siblings, deleted paths, and untrusted or untracked input. The citation driver
retains its full default suite; repository-only mode keeps real-tree checks,
and an injected verifier failure exits unsuccessfully. Its partition contract
traces individual checks through the actual runners. Mutated helper copies
prove that moving an existing real-tree check or adding one inside a fixture
runner is rejected; the real recurrence-scope check remains repository-only.

Performance Evidence: Citation command-scope durations are recorded as
ledger:prepr-selection-20260906-citations-full and
ledger:prepr-selection-20260906-citations-repository. These are unreplicated
command observations on the same code commit. Peer compilation overlapped the
repository-only observation, so the durations do not establish a comparative
speedup. There is no end-to-end pre-PR speedup claim. Fixture selection is
separately covered by committed-registry tests.

Observability Evidence: The runner reports WHOLE and REUSE command ownership,
retains blocking failures across reuse, and exposes the pre_pr_whole_module
mode and command durations in JSON. Stage elapsed time remains distinct from
the sum of concurrently executing command durations.

## Candidate ledger

| Candidate | Stage seconds | Expected saving | Cheapest proof | Old | New | Accuracy | Concurrency | Disposition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Core and package-doc ownership | Unmeasured | Remove repeated execution; duration unmeasured | Runner integration and shell scheduling tests | Outer and selected dispatch overlap | Runner owns core; selected package-doc gate owns docs | Mandatory scope and failed results retained | Process handshake preserves overlap | proven: Scheduling win |
| Citation fixture selection | ledger:prepr-selection-20260906-citations-full | Skip unrelated fixture work; duration unproven | Committed-registry selection and citation partition tests | Full suite on every primary selection | Fixtures selected by verifier inputs; real-tree checks remain primary | Baseline, floor, ledger and failure proof retained | No concurrency change | proven: scheduling hygiene |
| Citation wall-clock comparison | ledger:prepr-selection-20260906-citations-repository | No comparable estimate | Isolated repeated command samples still needed | Full observation | Repository-only observation with peer compilation | Both suites passed | Different contention prevents comparison | diagnostic-only |

Next measurement: isolate the real-tree citation checker, then attribute the
whole-module Go and documentation stages in a complete pre-PR run. Their
ordering as end-to-end bottlenecks has not been established on this branch.
