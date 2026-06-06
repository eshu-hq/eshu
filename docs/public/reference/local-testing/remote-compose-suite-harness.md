# Remote Compose Suite Harness

Use this page with
[Remote Collector E2E](remote-collector-e2e.md) after the representative stack
is running with pprof enabled.

Run the clean proof to build the shared manifest from live aggregate evidence:

```bash
scripts/e2e_remote_compose_suite.sh \
  --run-kind clean \
  --manifest /secure/local/eshu/e2e-clean-manifest.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --runtime-volume-proof /secure/local/eshu/clean-volume-proof.json \
  --corpus-coverage /secure/local/eshu/public-corpus-coverage.json \
  --readback-proof /secure/local/eshu/readback-proof.json \
  --corpus-mode representative \
  --repository-count 24 \
  --image-tag-candidate "$IMAGE_TAG" \
  --commit "$ESHU_COMMIT"
```

Then restart the same Compose project without pruning volumes and run the
preserved proof:

```bash
scripts/e2e_remote_compose_suite.sh \
  --run-kind preserved \
  --manifest /secure/local/eshu/e2e-preserved-manifest.json \
  --previous-manifest /secure/local/eshu/e2e-clean-manifest.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --api-key "$REMOTE_API_KEY" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL" \
  --runtime-volume-proof /secure/local/eshu/preserved-volume-proof.json \
  --corpus-coverage /secure/local/eshu/public-corpus-coverage.json \
  --readback-proof /secure/local/eshu/readback-proof.json \
  --corpus-mode representative \
  --repository-count 24 \
  --image-tag-candidate "$IMAGE_TAG" \
  --commit "$ESHU_COMMIT"
```

The `public-corpus-coverage.json` file is aggregate-only. It contains
`ecosystems` and `evidence_families` objects with the same row shape as the E2E
manifest. The file records whether the representative corpus covered npm, Go
modules, PyPI, Maven/Gradle, Composer, RubyGems, Cargo, NuGet, Terraform/IaC,
Kubernetes/IaC, image/SBOM, deployment, vulnerability, observability,
incident, and work-item evidence. Do not derive those rows from repository
count alone; they are part of the recorded corpus contract.

The `readback-proof.json` file is the public-safe aggregate API/MCP/CLI proof.
Generate it from operator-local bounded readback summaries and keep raw
transcripts outside the repository. Use `scripts/e2e_readback_parity.sh` to
turn those bounded summaries into the aggregate proof accepted by this harness.
Each surface must include status, checked, failed, truncated, unsupported,
missing-evidence, and ambiguous counters. If the harness runs without this
proof, reducer rows that otherwise have source and reducer counts remain
`fail` with an API/MCP readback reason instead of being treated as clean
evidence.
