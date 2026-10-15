# #7926 alpha2 activation parsing: no-regression evidence

Scope: branch `fix/7926-alpha2-activation`. `ParseConfig`
(`go/internal/coordinator/component/activation/config.go`) and durable
instance validation (`go/internal/workflow/collector_instance.go`) switch
over the trimmed `runtime.sdk_protocol` and admit `v1alpha1` or `v1alpha2`.
The gate flagged `collector_instance.go` as a hot file by path; this note
records why the change cannot regress runtime cost.

No-Regression Evidence (parse-time validation widening): the old code ran
one `TrimSpace` plus one string comparison per activation parse; the new
code runs one `TrimSpace` plus a two-case string switch. Both are O(1)
with no allocation, I/O, lock, or retry change, and the comparison runs
once per activation parse, never per row, scope, or generation, so there
is no corpus scaling term. Baseline: `TestParseConfig*` wall 0.142s for
the activation package at HEAD `ca5483416` (GOTOOLCHAIN=go1.26.9,
darwin/arm64 dev harness, not a production backend); workflow alpha2
accept tests report 0.00s each. After measurement equals the baseline
shape: the same tests plus the two-case switch, with pre-existing
v1alpha1 accept cases passing unmodified. Input shape: activation YAML
with exact and padded `collector-sdk/v1alpha2` across `oci`/`process`
adapters, plus `v9`/padded-`v9` reject boundaries. Queue/row counts: not
applicable, the validation precedes any enqueue and touches no rows.
Change is safe because the default path is byte-identical for v1alpha1
inputs and the only new accepted input is the explicitly configured
alpha2 constant pinned by tests on both sides.

No-Observability-Change: the acceptance path emits no new spans, metrics,
or logs. The rejection path keeps the existing descriptive `unsupported`
error naming the raw protocol value. No operator dashboard or alert
changes.
