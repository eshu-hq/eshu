# tfstate drift compose proof matrix

Captured: 2026-05-11T14:21:27Z
Compose project: `eshu-tfstate-drift-166-23368`
Worktree HEAD: `2cfc31b` on feature/tfstate-drift-proof-166

## Phase 3.5 enqueue log

```
2026/05/11 14:21:19 config_state_drift_intents_enqueued count=4 duration_s=0.00
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
resolution-engine-1  | {"timestamp":"2026-05-11T14:21:20.239988131Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5","generation_id":"gen:state:added-in-state","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_state","drift.address":"aws_s3_bucket.unmanaged","trace_id":"6a3d304da5a33d0a6c61d415fc179d32","span_id":"91574ef89ccf0b27","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:21:20.240239549Z","severity_text":"WARN","message":"drift candidate rejected","domain":"config_state_drift","scope_id":"state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e","generation_id":"gen:state:ambiguous","failure_class":"ambiguous_backend_owner","rejection.reason":"ambiguous backend owner","trace_id":"01ee351a4e165ef5539e2b90e6c60bab","span_id":"991eaee829aea0c2","severity_number":13,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:21:20.242601897Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38","generation_id":"gen:state:removed-from-state:current","drift.pack":"terraform_config_state_drift","drift.kind":"removed_from_state","drift.address":"aws_s3_bucket.was_there","trace_id":"2c7bdb958313a13aee27d5c47fa82169","span_id":"ea92c7712a45a8c2","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:21:20.243020733Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66","generation_id":"gen:state:added-in-config","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_config","drift.address":"aws_s3_bucket.declared","trace_id":"5d9b1575ae047d3b0e69f3a3535ed64b","span_id":"25f41e62c7cd8c6c","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
```
