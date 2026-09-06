# Pre-PR selection evidence

No-Regression Evidence: The [runner integration tests](../../../go/cmd/ci-gates/execute_prepr_test.go)
exercise the actual CLI, mandatory whole-module commands on unselected paths,
command-result reuse, failed-result propagation, and rejection of missing or
CI-only core commands before execution. A bounded process handshake checks
that build and vet overlap the ordered formatting and lint stage.

The shell scheduling tests preserve FULL/FAST routing. Classifier and Git
collector regressions cover skill Markdown, executable and configuration
siblings, deleted paths, and untrusted or untracked input. The citation driver
retains its full default suite; repository-only mode keeps real-tree checks,
and an injected verifier failure exits unsuccessfully.

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
