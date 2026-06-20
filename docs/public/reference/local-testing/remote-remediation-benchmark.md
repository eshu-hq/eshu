# Remote Remediation Benchmark

Use this benchmark after collector, reducer, graph, or query changes that can
affect full-chain supply-chain remediation proof. It promotes the target-story
remote E2E proof into a repeatable measurement from a known CVE/package anchor
to API/MCP remediation packet parity.

The benchmark wraps `scripts/verify_remote_e2e_target_story.sh`; it does not
start collectors, change reducer concurrency, create graph writes, or replace
the remote Compose runtime-state gate. Run it only after the remote stack has
already converged and the target-story manifest is available in the private
operator environment.

```bash
export ESHU_REMOTE_E2E_TARGET_STORY_FILE=/path/to/private-target-story.json
export ESHU_REMOTE_E2E_API_BASE_URL=http://127.0.0.1:8080/api/v0
export ESHU_REMOTE_E2E_MCP_URL=http://127.0.0.1:8081/mcp/message
export ESHU_REMOTE_E2E_IMAGE_DIGEST=sha256:<image-digest>

scripts/verify-remote-e2e-remediation-benchmark.sh \
  --artifacts .proof/remediation-benchmark
```

The target-story manifest must include:

```json
{
  "target_repository_id": "operator-local",
  "expected_image_digest": "sha256:<digest>",
  "remediation_benchmark": {
    "cve_id": "CVE-YYYY-NNNN",
    "package_id": "pkg:ecosystem/name"
  }
}
```

Keep the manifest and raw transcripts outside the repository. The public
artifacts contain only commit SHA, image digest, issue references, wall time,
queue state, fact-count and graph-write aggregates, API/MCP parity state, and
missing-evidence counts/classes.

If `/api/v0/index-status` does not expose public-safe `fact_counts` and
`graph_writes`, provide an operator-local aggregate JSON file through
`ESHU_REMOTE_E2E_BENCHMARK_COUNTERS_FILE`. The file must contain only aggregate
objects:

```json
{
  "fact_counts": {
    "fact_records": 123,
    "supply_chain": 12
  },
  "graph_writes": {
    "total": 9,
    "relationship_edges": 4
  }
}
```

The benchmark fails if the queue is not terminal, if fact or graph-write counts
are missing, if the API and MCP `explain_supply_chain_impact` packets disagree,
or if the explanation does not reach an owner/remediation packet. Raw API
responses, MCP responses, provider values, URLs, local paths, tokens, account
ids, repository names, and package names stay operator-local.

Focused local verifier:

```bash
bash scripts/test-verify-remote-e2e-remediation-benchmark.sh
```

No-Observability-Change: this wrapper adds no runtime surface, queue domain,
worker, graph write, metric, span, log field, or API/MCP contract. Operators
continue to diagnose the live run through the existing remote Compose health,
status, pprof, queue, fact, graph, and target-story readbacks.
