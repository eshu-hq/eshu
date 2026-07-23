# prod-hardcoded-secrets — production validation

Capability: `security.hardcoded_secrets` (tool
`investigate_hardcoded_secrets`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 2500`, `max_truth_level: derived`.

## Claim validated

Bounded redacted secret-candidate investigation over the content index;
findings are redacted by default, with citations required for exact redacted
context rather than an unbounded content dump.

## Committed reproducible evidence

**Handler contract, redaction, and offset bounds** —
`go/internal/query/code_security_secrets_test.go`:
`TestHandleHardcodedSecretInvestigationReturnsRedactedPromptPacket` and
`TestHandleHardcodedSecretInvestigationRejectsHugeOffset`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleHardcodedSecretInvestigation -count=1
```

## Notes

No private data: the cited tests use synthetic secret-pattern fixtures; the
capability itself redacts findings by contract, so this artifact does not
reproduce any real secret value.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
