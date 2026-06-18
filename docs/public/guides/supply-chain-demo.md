# Supply-Chain Demo Runbook

This runbook walks the supply-chain traceability story end to end using the
self-contained, fully synthetic demo corpus in
[`examples/supply-chain-demo/`](https://github.com/eshu-hq/eshu/tree/main/examples/supply-chain-demo).
It is the runnable companion to
[Supply-Chain Traceability](../supply-chain-traceability.md) (issue
[#3019](https://github.com/eshu-hq/eshu/issues/3019)).

Everything here is **synthetic and deterministic**: no real CVE ids, package
names, registries, credentials, or provider data. The package
`synthetic-vulnerable-npm` and the advisory `CVE-2026-SYNTHETIC-NPM` /
`GHSA-synthetic-npm-0001` do not exist on any real feed.

## What runs offline vs what needs the stack

Be honest about this when presenting. Claiming a step works when it does not is a
product failure.

| Step | Offline? | Why |
| --- | --- | --- |
| Repository dependency scan (`eshu vuln-scan repo app`) | **Yes** | The scan boots a self-contained local Eshu owner and reads the committed manifest + lockfile. No Docker Compose, no collectors. |
| Missing-evidence refusal (`eshu vuln-scan repo missing-evidence`) | **Yes** | Eshu refuses to promote a finding it has no owned advisory for; the differentiator needs no feeds. |
| SARIF / VEX export of the offline scan | **Yes** | The exporter renders whatever finding set the scan produced. |
| Promoted CVE finding (`ready_with_findings` with `CVE-2026-SYNTHETIC-NPM`) | **No** | Promotion requires **owned advisory facts** for the synthetic package; no real feed knows a synthetic CVE, so you seed them via the Compose stack and collectors. |
| Image digest → SBOM subject attachment → workload → impact | **No** | Requires the Compose stack, the container-registry / SBOM collectors, and a workload correlated to the image digest. |

Offline you can prove the **dependency scan and the refusal**. The **full chain
with the promoted CVE finding requires the Compose stack plus seeded advisory
facts**.

## Part A — Offline

### A0. Clone and build

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu/go
go build -o ../bin/eshu ./cmd/eshu
cd ..
export PATH="$PWD/bin:$PATH"
```

### A1. Scan the vulnerable repository

```bash
eshu vuln-scan repo examples/supply-chain-demo/app --json
```

The scan boots a local Eshu service and discovers `synthetic-vulnerable-npm@1.0.0`
from the lockfile. Offline, with no advisory feed seeded, the readiness state is
**not** `ready_with_findings` — Eshu reports that advisory evidence is not owned.
That is correct: Eshu does not invent a finding from a package alone. To see the
promoted `ready_with_findings` result, seed advisory facts (Part B) or run the
automated demo test below.

### A2. Demonstrate the refusal path

```bash
eshu vuln-scan repo examples/supply-chain-demo/missing-evidence --json
echo "exit code: $?"
```

The dependency is present but Eshu owns no advisory for it, so the scanner returns
readiness state **`evidence_incomplete`** with `missing_evidence` containing
**`advisory_sources`** and exits with **code 4**. This is the refusal the launch
story is built on.

### A3. Export the offline scan as SARIF

```bash
eshu vuln-scan repo examples/supply-chain-demo/app --export sarif > app.sarif.json
```

See [SARIF Export](../reference/sarif-export.md) for the schema and downstream
consumers.

## Part B — Full chain (Compose stack + seeded advisory facts)

This part is not offline. Bring up the stack first using
[Docker Compose](../run-locally/docker-compose.md).

### B1. Build the synthetic image

```bash
docker build -f examples/supply-chain-demo/Dockerfile \
  -t synthetic-supply-chain-demo-app:1.0.0 examples/supply-chain-demo
docker image inspect synthetic-supply-chain-demo-app:1.0.0 \
  --format '{{ index .RepoDigests 0 }}'
```

The template workflow `examples/supply-chain-demo/ci/build-image-and-sbom.yml`
performs this build, generates a CycloneDX SBOM with `anchore/sbom-action`, and
attaches it as an OCI referrer with `cosign`. It is a template to copy into the
demo application's own repository at `.github/workflows/`; it is
`workflow_dispatch`-only.

### B2. Seed advisory and package facts

To promote `synthetic-vulnerable-npm` into `ready_with_findings`, the reducer must
own a `vulnerability.advisory` fact for `CVE-2026-SYNTHETIC-NPM` /
`GHSA-synthetic-npm-0001` affecting `synthetic-vulnerable-npm < 1.0.1`, plus
`package.registry` and `package.consumption` facts for the observed `1.0.0`. In a
real deployment these come from the advisory and package collectors; for a
synthetic package you seed them yourself. See
[Security Intelligence](../reference/security-intelligence.md) for the advisory
fact shape. This is deployment/collector work and is not scripted in the demo.

### B3. Attach the SBOM and correlate a workload

`sbom/app.cdx.json` carries `metadata.component` as a container with the synthetic
subject digest. Eshu attaches an SBOM to an image only when the SBOM's **subject
digest matches an owned image identity** — presence alone does not attach.
Replace the placeholder digest with the digest from B1, attach the SBOM as an OCI
referrer, and correlate a workload that runs the image.

### B4. Ask the chain from Claude Code

With the stack up and facts seeded, ask the MCP/CLI surface for the impact finding
and its evidence chain. The promoted answer is `ready_with_findings` with
`CVE-2026-SYNTHETIC-NPM`, the dependency path, the image digest, and the workload.

## Proving the demo

The CLI flow is guarded by a focused Go test that runs `eshu vuln-scan repo`
against the demo fixtures (stubbing only the advisory envelope):

```bash
cd go && go test ./cmd/eshu -run TestRunVulnScanRepoSupplyChainDemo -count=1
```

It asserts `app/` reaches `ready_with_findings` with `CVE-2026-SYNTHETIC-NPM`
(exit 3) and `missing-evidence/` reaches `evidence_incomplete` with
`missing_evidence=[advisory_sources]` (exit 4). The test reads the fixtures
directly from `examples/supply-chain-demo/`, so the runbook and the test cannot
drift.

## Not covered

- The 10–15 minute screen recording is a manual deliverable.
- Seeding advisory facts, attaching the SBOM to a live image, and correlating a
  workload (B2/B3) are deployment/collector steps that depend on the running
  stack rather than this static corpus.

## Related

- [Supply-Chain Traceability](../supply-chain-traceability.md)
- [SARIF Export](../reference/sarif-export.md)
- [Vulnerability Scanner Read Contract](../reference/vulnerability-scanner-read-contract.md)
- [Value-Flow Emission](../reference/value-flow-emission.md)
- [Docker Compose](../run-locally/docker-compose.md)
