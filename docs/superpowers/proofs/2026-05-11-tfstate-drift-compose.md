# tfstate drift compose proof matrix

Captured: 2026-05-11T14:32:40Z
Compose project: `eshu-tfstate-drift-166-71967`
Worktree HEAD: `9c26e2d` on feature/tfstate-drift-proof-166

## Phase 3.5 enqueue log

```
2026/05/11 14:32:32 config_state_drift_intents_enqueued count=4 duration_s=0.00
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
resolution-engine-1  | {"timestamp":"2026-05-11T14:32:33.378110314Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:33f0f3a35fa8bf42780910e64cc72a8977797dac9eecbbc50a623687bc79be38","generation_id":"gen:state:removed-from-state:current","drift.pack":"terraform_config_state_drift","drift.kind":"removed_from_state","drift.address":"aws_s3_bucket.was_there","trace_id":"5ad85effc3e7307b6ba11625a834c95b","span_id":"1c7153820873a577","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:32:33.378581899Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:01c90e0b47d80dfc362bb2647d1306c5889b316d1c3b37ad91a49ccbb7e16ae5","generation_id":"gen:state:added-in-state","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_state","drift.address":"aws_s3_bucket.unmanaged","trace_id":"474a32375eaea763a21e232f7f5c07c6","span_id":"bf42c69567b60c2b","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:32:33.379151693Z","severity_text":"WARN","message":"drift candidate rejected","domain":"config_state_drift","scope_id":"state_snapshot:s3:6ef42db5dda508cb600e123a97ef2e128947bd582afac01e2e64a19dddf7ce4e","generation_id":"gen:state:ambiguous","failure_class":"ambiguous_backend_owner","rejection.reason":"ambiguous backend owner","trace_id":"83c50ac18d052272fe83874ae5abcf9d","span_id":"184b6141ce5a8f49","severity_number":13,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
resolution-engine-1  | {"timestamp":"2026-05-11T14:32:33.385011008Z","severity_text":"INFO","message":"drift candidate admitted","domain":"config_state_drift","scope_id":"state_snapshot:s3:ac3219927f950b4a0a57e415168765f0c49fccaa862b32e0906cacc29548eb66","generation_id":"gen:state:added-in-config","drift.pack":"terraform_config_state_drift","drift.kind":"added_in_config","drift.address":"aws_s3_bucket.declared","trace_id":"c67e3be12566ab72dfac23a2d26bb628","span_id":"26fb28ed3130f305","severity_number":9,"service_name":"reducer","service_namespace":"eshu","component":"reducer","runtime_role":"reducer"}
```
