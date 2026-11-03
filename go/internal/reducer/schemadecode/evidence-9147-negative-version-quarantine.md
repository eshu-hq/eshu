# Negative version-part quarantine (#9147)

No-Regression Evidence: `DecodeRuntimeDeclarationVersion` adds three
integer comparisons (nil/sign check per version part) per decoded fact; valid
facts take the identical projection path as before, so there is no new I/O,
lock, or ordering change in the decode path. Measured after (`go test -count=1` on this branch):
`reducer/runtime/declaration` 0.005s, `reducer/schemadecode` 0.016s,
`query/runtime/declaration` 0.008s, all green (no pre-change suite-time
baseline was taken; the structural bound above is the regression argument).
Backend/version: local Go reducer unit path, no graph backend involved
(decode-only, pre-projection). Input shape: single-fact envelopes built by
`versionEnvelopeParts`. Terminal counts: 1 projected + 1 quarantined
(`input_invalid`, field named) per subtest, 3/3 subtests green.

Observability Evidence: a quarantined negative part surfaces through the
existing instruments — `eshu_dp_reducer_input_invalid_facts_total` counter,
the per-fact error log in `RecordQuarantinedFacts` naming fact and field,
`input_invalid_count` slog line, and the `input_invalid_facts` SubSignal. No
new metric, span, route, or log key was added.
