# tfstate drift compose proof matrix

Captured: 2026-05-11T14:42:52Z
Compose project: `eshu-tfstate-drift-166-54295`
Worktree HEAD: `e558ff3` on feature/tfstate-drift-proof-166

## Phase 3.5 enqueue log

```
2026/05/11 14:42:46 config_state_drift_intents_enqueued count=4 duration_s=0.00
```

## Counter snapshot (after drain)

```
eshu_dp_correlation_drift_detected_total{drift_kind="added_in_config",otel_scope_name="eshu/go/data-plane",otel_scope_version="",pack="terraform_config_state_drift",rule="admit-drift-evidence",service_name="reducer",service_namespace="eshu"} 1
eshu_dp_correlation_drift_detected_total{drift_kind="added_in_state",otel_scope_name="eshu/go/data-plane",otel_scope_version="",pack="terraform_config_state_drift",rule="admit-drift-evidence",service_name="reducer",service_namespace="eshu"} 1
eshu_dp_correlation_drift_detected_total{drift_kind="removed_from_state",otel_scope_name="eshu/go/data-plane",otel_scope_version="",pack="terraform_config_state_drift",rule="admit-drift-evidence",service_name="reducer",service_namespace="eshu"} 1
eshu_dp_correlation_rule_matches_total{otel_scope_name="eshu/go/data-plane",otel_scope_version="",pack="terraform_config_state_drift",rule="match-config-against-state",service_name="reducer",service_namespace="eshu"} 3
```

## Structured log excerpts (drift admit + reject)

```json
resolution-engine-1  | {"timestamp":"2026-05-11T14:42:46.741030915Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5","generation_id":"gen:state:added-in-state","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_state","drift.address":"aws_s3_bucket.unmanaged","trace_id":"4b562df4e792edb6c6a8384a5c9a0fa3","span_id":"720d80f1832ab79f","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:42:46.741859544Z","severity_text":"WARN","message":"drift candidate rejected","domain":"config_state_drift","scope_id":"state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e","generation_id":"gen:state:ambiguous","failure_class":"ambiguous_backend_owner","rejection.reason":"ambiguous backend owner","trace_id":"46820a87c17bc7b7667f4d5a2f96f483","span_id":"4adf7a41465cc69a","severity_number":13,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:42:46.744253637Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38","generation_id":"gen:state:removed-from-state:current","drift.pack":"terraform_config_state_drift","drift.kind":"removed_from_state","drift.address":"aws_s3_bucket.was_there","trace_id":"e4d9f4974eb29a1a6edaed296bb44d7d","span_id":"438d3bd698c0801c","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:42:46.747562693Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66","generation_id":"gen:state:added-in-config","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_config","drift.address":"aws_s3_bucket.declared","trace_id":"cdeb4cff06524dac83f781b7152c3542","span_id":"a1cbee4886977b69","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
```
