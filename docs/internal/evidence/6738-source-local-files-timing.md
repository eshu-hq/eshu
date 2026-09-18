# Source-local File group timing for #6738

Root-Cause Evidence: ops-qa source-local projector work has files-phase graph
write timeouts, but the existing canonical-phase and grouped-query durations do
not attribute them to one File statement template or to the transaction's
post-callback interval. The timeout symptom does not establish a query or commit
root cause.

Performance Evidence: a throwaway five-statement timing shim on an Apple M5 Max
measured baseline median 1.759 ns/op, disabled median 3.195 ns/op with 0
allocations, and enabled JSON logging median 6.828 microseconds/op with 32
allocations. This is only a no-backend theory shim, not a throughput claim. The
implemented opt-in probe's five-statement, no-backend benchmark on the same
machine, rerun after the final instrumentation edit, measured disabled
1.868-2.075 ns/op with 0 allocations in five samples and enabled
10.140-10.837 microseconds/op with 63 allocations. The two harnesses
are different and their totals must not be compared as a speedup or regression.
The diagnostic is disabled by default. The local live test below checks graph
truth for one fixture. Deployed performance and the ops-qa timeout cause require
a bounded replay after the diagnostic image is deployed.

Observability Evidence: `ESHU_NORNICDB_PROFILE_FILE_GROUPS=true` logs a fixed
File template ID, per-process group call ID, managed transaction callback
attempt number, statement index/count, row count, `tx.Run` and
`Result.Consume` wall time only when called, and one final group outcome
plus post-callback wall time only after a successful callback. Both event
families carry `pipeline_phase=projection`. The log omits Cypher text,
parameters, paths, summaries, and raw errors. Per-statement success is only an attempt; the managed transaction
may replay its callback. Post-callback time includes possible retry/backoff and
transport work, so it cannot by itself prove slow commit. Existing
`eshu_dp_neo4j_query_duration_seconds` covers the whole group.

Live local proof used the v1.3.3 tag source at
`a9956536c2cc902e3463aeb9fbc43c695e3dabb0`, built with
`noui nolocalllm` and launched over Bolt with an isolated scratch data
directory. `ESHU_FILE_GROUP_PROBE_LIVE=1 ESHU_NEO4J_URI="$LOCAL_BOLT_URI"
ESHU_NEO4J_DATABASE=nornic go test ./cmd/ingester -run
'^TestFileGroupProbeLive$' -count=1 -v` passed: the nested first-generation
File template emitted sanitized statement and final group events, and a
separate read found one File, one Repository containment edge, and one
Directory containment edge. The probe's reported statement completion was
checked against graph truth, not accepted on its own.

An earlier fixture seeded Repository and Directory through implicit
`session.Run` writes. Its immediately following managed File transaction saw
zero matching ancestors and returned success with no File, both with the
probe enabled and disabled. An isolated scratch experiment showed the same
visibility gap and that seeding through managed `ExecuteWrite` avoids it.
The test now seeds through that managed boundary. The underlying cause of
the implicit-to-managed visibility gap is not established here; this does
not claim to fix production graph truth. Pulling the pinned Docker image
failed because Docker ran out of space, so this live proof uses a local build
of the v1.3.3 tag rather than the deployed OCI artifact.

The next proof is a bounded live replay that validates graph truth and correlates the slow template and operation stage with backend profiling. No query rewrite or timeout change follows from the timeout count alone.
