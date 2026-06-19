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

The Dockerfile does not run `npm ci` because `synthetic-vulnerable-npm` is not a
real registry package. It copies a minimal local runtime module into
`node_modules/` so the image starts while the repository scan still treats
`package.json` and `package-lock.json` as the dependency source of truth.

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

## Image-identity proof (the `image_ref` sub-hop)

The full-chain proof anchors findings to a workload via the K8s manifest but
does **not** populate `image_ref` (the OCI-registry collector is registry/
ECR-gated). `examples/supply-chain-demo/scripts/run-image-identity-proof.sh`
completes that sub-hop: it proves a registry-observed image digest surfaces as
`image_ref` on the impact finding, using the demo's synthetic
`synthetic-vulnerable-npm` package and a synthetic advisory — **no real registry
and no real OSV feed**.

It seeds the raw collector facts with a tier-1 SQL seed
(`scripts/seed-image-identity-facts.sql`, modeled on the tier-1 pattern in
`tests/fixtures/tfstate_drift/seed.sql`) and lets the live reducers derive the
correlation facts:

| Seeded (raw collector facts) | Reducer-derived at runtime |
| --- | --- |
| `oci_registry.image_manifest` | `reducer_container_image_identity` (`exact_digest`, `image_ref`) |
| `sbom.document` + `sbom.component` | `reducer_sbom_attestation_attachment` |
| `attestation.statement` | `reducer_supply_chain_impact_finding` (`image_ref`) |
| `vulnerability.cve` + `vulnerability.affected_package` (synthetic advisory) | |

The corpus K8s Deployment references the **same** digest
(`demo.invalid/vuln-demo-app@sha256:1111…1111`), which is what lets the
image-identity reducer classify it as `exact_digest` and emit `image_ref`. The
seed contains no secrets: `demo.invalid` is RFC 6761 reserved, the digest is an
obvious placeholder, and the advisory exists on no feed.

```bash
ESHU_SRC=/path/to/eshu \
  examples/supply-chain-demo/scripts/run-image-identity-proof.sh
```

It asserts a finding whose `image_ref` and `subject_digest` equal the seeded
image identity. A daemon-free Go test proves the same reducer join offline:

```bash
cd go && go test ./internal/reducer \
  -run TestBuildSupplyChainImpactFindingsDemoImageIdentityHop -count=1
```

## Image-identity proof against a real localhost TLS registry (no cloud account)

The seeded proof above does not exercise the OCI-registry collector's actual
transport: it injects `oci_registry.image_manifest` facts rather than fetching
them. `examples/supply-chain-demo/scripts/run-oci-localtls-identity-proof.sh`
closes that gap without a cloud account by standing up a real `registry:2` over
TLS on `127.0.0.1` and pointing the collector at it with a custom CA:

1. Mints an **ephemeral** CA + server cert with `openssl` at runtime (never
   committed; removed on exit).
2. Runs `registry:2` over TLS on `127.0.0.1` using that cert (orbstack docker).
3. Builds the demo's synthetic image and pushes it to the local TLS registry.
4. Runs the env-gated Go proof `TestLiveLocalTLSRegistryImageIdentity`, which
   drives the real OCI collector `Source` against the registry trusting the
   ephemeral CA via the target's `tls_ca_cert_path`, then asserts an
   image-identity manifest fact carrying the registry-observed `sha256` digest.

```bash
ESHU_SRC=/path/to/eshu \
  examples/supply-chain-demo/scripts/run-oci-localtls-identity-proof.sh
```

The collector trusts the registry through the `tls_ca_cert_path` target field
documented in
[Environment Collectors](../reference/environment-collectors.md#oci-registry-collector);
the system-roots default correctly rejects the same registry, which the negative
test `TestSourceRejectsLocalTLSRegistryWithoutCustomCA` proves. Everything is
synthetic and local: the registry is `127.0.0.1`, the image is the demo's own
synthetic app, and all key material lives in a temp dir.

## Not covered

- The 10–15 minute screen recording is a manual deliverable.
- A **real** OCI-registry collector run for the image-identity hop's transport is
  now covered for a localhost TLS registry by the proof script directly above;
  the **seeded** proof remains the path that exercises the full reducer join to
  `image_ref` (the synthetic advisory is on no feed). The reducer join that emits
  `image_ref` is the real, unmocked path in both.
- Attaching the SBOM to a live image and correlating a workload (B2/B3) remain
  deployment/collector steps that depend on the running stack rather than this
  static corpus.

## Related

- [Supply-Chain Traceability](../supply-chain-traceability.md)
- [SARIF Export](../reference/sarif-export.md)
- [Vulnerability Scanner Read Contract](../reference/vulnerability-scanner-read-contract.md)
- [Value-Flow Emission](../reference/value-flow-emission.md)
- [Docker Compose](../run-locally/docker-compose.md)
