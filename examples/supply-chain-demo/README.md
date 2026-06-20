# Supply-Chain Demo (issue #3019)

A self-contained, fully synthetic demo of Eshu's supply-chain traceability
story: from a vulnerable dependency in a repository, to the refusal Eshu returns
when it does not own the advisory evidence, up to the full
CVE → advisory → package → manifest → lockfile → image → SBOM → workload →
impact chain.

Everything here is **synthetic and deterministic**. There are no real CVE ids,
real package names, real registries, credentials, or provider data. The
"vulnerable" package `synthetic-vulnerable-npm` and the advisory
`CVE-2026-SYNTHETIC-NPM` / `GHSA-synthetic-npm-0001` do not exist on any real
feed; they mirror the convention in
`go/cmd/eshu/testdata/vuln_scan_repo_fixtures/`.

## What is in this directory

| Path | What it is |
| --- | --- |
| `app/` | Synthetic vulnerable repo: `package.json` + `package-lock.json` pinning `synthetic-vulnerable-npm@1.0.0`, plus a tiny `server.js` that imports it. |
| `app/.eshuignore` | Keeps the scan deterministic (ignores `node_modules/`, build output). |
| `missing-evidence/` | Variant repo: the dependency is present but **no advisory is owned**, used to demonstrate the refusal path. |
| `docker-stubs/` | Local stand-in for the synthetic vulnerable package so the demo image can start without fetching from a real registry. |
| `sbom/app.cdx.json` | Static CycloneDX 1.4 SBOM whose `metadata.component` is a container with a synthetic `sha256:…` subject digest, listing the synthetic vulnerable component. |
| `Dockerfile` | Builds the synthetic demo image so the chain has a real image identity (digest). |
| `fixtures/full-chain-proof-output.json` | Public sanitized proof fixture that records the required full-chain evidence nodes, refusal variant, and p95 matrix shape. |
| `test/verify-full-chain-proof-output.sh` | Example-level verifier for the proof fixture; it asserts 7+ evidence nodes, refusal semantics, executable proof scripts, timing fields, and public-safe output. |
| `test/test-verify-full-chain-proof-output.sh` | Regression harness for the fixture verifier, including the pipefail path for public-safety scans. |
| `scripts/full-chain-proof.sh` | Unified live proof entrypoint that runs the split proof scripts and writes the timing artifact. |

## Honesty: what runs offline vs what needs the stack

Read this before recording or presenting. Claiming a step works when it does not
is a product failure.

| Step | Runs fully offline? | Why |
| --- | --- | --- |
| Repository dependency scan (`eshu vuln-scan repo app`) | **Yes** | The scan boots a self-contained local Eshu owner and reads the committed manifest + lockfile. No Docker Compose, no collectors. |
| Missing-evidence refusal (`eshu vuln-scan repo missing-evidence`) | **Yes** | Eshu refuses to promote a finding it has no owned advisory for; this is the differentiator and it needs no feeds. |
| SARIF / VEX export of the offline scan (`--export sarif`) | **Yes** | The exporter renders whatever finding set the scan produced. |
| Full CVE → impact chain for the vulnerable app (`ready_with_findings` with `CVE-2026-SYNTHETIC-NPM`) | **No** | Promoting that finding requires **owned advisory facts** for the synthetic package. No real feed knows a synthetic CVE, so you must seed the advisory + package-registry facts via the Docker Compose stack and collectors. |
| Image digest → SBOM subject attachment → workload → impact | **No** | Requires the Compose stack, the container-registry / SBOM collectors, and a workload correlated to the image digest. |

In short: **offline you can prove the dependency scan and the refusal**. The
**full chain with the promoted CVE finding requires the Compose stack + seeded
advisory facts**. The automated test for this demo
(`go/cmd/eshu/vuln_scan_supply_chain_demo_test.go`) exercises the CLI flow
against these exact fixtures and stubs the advisory envelope, which is how it can
assert `ready_with_findings` without a live feed.

## Part A — Offline (no Docker, no collectors)

These steps need only a built `eshu` binary.

### A0. Clone and build

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu/go
go build -o ../bin/eshu ./cmd/eshu
cd ..
export PATH="$PWD/bin:$PATH"
```

### A1. Scan the vulnerable repository (offline)

```bash
eshu vuln-scan repo examples/supply-chain-demo/app --json
```

What you will see **offline**: the scan boots a local Eshu service, discovers
`synthetic-vulnerable-npm@1.0.0` from the lockfile, and reports readiness. Because
no advisory feed is seeded for the synthetic package, the offline readiness state
is **not** `ready_with_findings` — it reports that advisory evidence is not owned.
That is correct behaviour: Eshu does not invent a finding from a package alone.

To see the *promoted* `ready_with_findings` result with `CVE-2026-SYNTHETIC-NPM`,
seed advisory facts via the stack (Part B) or run the automated demo test
described below.

### A2. Demonstrate the refusal path (offline)

```bash
eshu vuln-scan repo examples/supply-chain-demo/missing-evidence --json
echo "exit code: $?"
```

The dependency is present but Eshu owns no advisory for it, so the scanner
returns readiness state **`evidence_incomplete`** with
`missing_evidence` containing **`advisory_sources`**, and exits with **code 4**.
This is the refusal the launch story is built on: Eshu tells you it cannot
answer rather than guessing.

### A3. Export the offline scan as SARIF

```bash
eshu vuln-scan repo examples/supply-chain-demo/app --export sarif > app.sarif.json
```

See [SARIF Export](../../docs/public/reference/sarif-export.md) for the schema
and how to feed it into GitHub Code Scanning or a SIEM.

## Part B — Full chain (Docker Compose stack + seeded advisory facts)

This part is **not** offline. It needs the Compose stack, the advisory and
package-registry collectors, and (for the image half) a built image plus a
workload correlated to the image digest. Follow
[Docker Compose](../../docs/public/run-locally/docker-compose.md) to bring up the
stack first.

### B1. Build the synthetic image (produces an image identity)

```bash
# Pin the base by digest first for a reproducible image digest (see Dockerfile).
docker build -f examples/supply-chain-demo/Dockerfile \
  -t synthetic-supply-chain-demo-app:1.0.0 examples/supply-chain-demo
docker image inspect synthetic-supply-chain-demo-app:1.0.0 \
  --format '{{ index .RepoDigests 0 }}'
```

The Dockerfile does not run `npm ci` because `synthetic-vulnerable-npm` is not a
real registry package. It copies a minimal local runtime module into
`node_modules/` so the image starts while the repository scan still treats
`package.json` and `package-lock.json` as the dependency source of truth.

The template workflow `ci/build-image-and-sbom.yml` performs this build and
generates a CycloneDX SBOM with `anchore/sbom-action`, attaching it as an OCI
referrer with `cosign`. It is a template, not an active eshu workflow: copy it
into the demo application's own repository at
`.github/workflows/build-image-and-sbom.yml`. It is `workflow_dispatch`-only so
a consumer opts into when it runs.

### B2. Seed advisory + package-registry facts

To promote `synthetic-vulnerable-npm` into `ready_with_findings`, the reducer must
own:

- a `vulnerability.advisory` fact for `CVE-2026-SYNTHETIC-NPM` /
  `GHSA-synthetic-npm-0001` affecting `synthetic-vulnerable-npm < 1.0.1`, and
- `package.registry` + `package.consumption` facts for the observed `1.0.0`.

In a real deployment these come from the advisory collectors (OSV, NVD, etc.) and
the package collectors. For a synthetic package you seed them yourself; see
[Security Intelligence](../../docs/public/reference/security-intelligence.md) for
the advisory fact shape. **This step is deployment/collector work and is not
scripted here** — do not claim the promoted finding without it.

### B3. Attach the SBOM to the image and correlate a workload

`sbom/app.cdx.json` carries `metadata.component` as a container with the synthetic
subject digest `sha256:1111…1111`. Eshu attaches an SBOM to an image only when the
SBOM's **subject digest matches an owned image identity** — SBOM presence alone
does not attach. Replace the placeholder digest with the digest from B1, attach
the SBOM as an OCI referrer, and correlate a workload that runs the image so the
chain reaches `workload → impact`.

### B4. Ask the chain from Claude Code

With the stack up and facts seeded, ask the MCP/CLI surface for the impact
finding and its evidence chain. The promoted answer is `ready_with_findings` with
`CVE-2026-SYNTHETIC-NPM`, the dependency path, the image digest, and the workload.

## Proving the demo (automated test)

The flow above is guarded by a focused Go test that runs the real
`eshu vuln-scan repo` command against these fixtures (stubbing only the advisory
envelope, exactly as the existing corpus harness does):

```bash
cd go && go test ./cmd/eshu -run TestRunVulnScanRepoSupplyChainDemo -count=1
```

It asserts:

- `app/` reaches `ready_with_findings` with `CVE-2026-SYNTHETIC-NPM` (exit 3).
- `missing-evidence/` reaches `evidence_incomplete` with
  `missing_evidence=[advisory_sources]` (exit 4).

These fixtures are the single source of truth: the test reads them directly from
this directory, so the runbook and the test cannot drift.

## Full-chain proof fixture

`fixtures/full-chain-proof-output.json` is the committed, redacted proof artifact
for the #3129 acceptance shape. It records the evidence-node chain, the
missing-evidence refusal variant, the required p95 timing matrix fields, and the
proof boundary between the live Compose stack script, the seeded image-identity
reducer proof, and the localhost TLS OCI collector proof.

Verify it with:

```bash
examples/supply-chain-demo/test/verify-full-chain-proof-output.sh
examples/supply-chain-demo/scripts/full-chain-proof.sh --verify-fixture-only
```

The fixture is intentionally public and deterministic. It is not a replacement
for rerunning `scripts/full-chain-proof.sh` against a live Compose stack when
closing #3129; live p95 values must come from that run.

## Unified full-chain proof entrypoint

`scripts/full-chain-proof.sh` is the real-stack proof entrypoint for #3129. By
default it runs the live Compose repo-to-workload proof, the seeded image
identity reducer proof, and the localhost TLS OCI collector proof, then writes a
single JSON artifact with one timing row per proof step. Use
`--verify-fixture-only` for a cheap shape check that does not start Compose.

```bash
ESHU_SRC=/path/to/eshu examples/supply-chain-demo/scripts/full-chain-proof.sh
```

## Scripted full-chain proof (live stack)

`scripts/run-full-chain-proof.sh` drives the **repo → owned package → OSV
advisory → published impact finding → workload** chain against a local Docker
Compose stack and asserts a reducer-published finding that is anchored to a
workload (with the SBOM attestation attached). It transcribes the recipe proven
live in the #3014 + #3061 runs, using a real registry package with a real
advisory (`lodash` 4.17.11) for the owned evidence (a synthetic package has no
real advisory to join) plus a K8s Deployment manifest in the same repo for the
workload anchor.

```bash
ESHU_SRC=/path/to/eshu examples/supply-chain-demo/scripts/run-full-chain-proof.sh
```

## Scripted image-identity proof (live stack, seeded image identity)

`scripts/run-image-identity-proof.sh` completes the OCI image-**identity**
sub-hop that the full-chain proof above deliberately skips: it proves a
registry-observed image digest surfaces as **`image_ref`** on the impact
finding. It uses the demo's own synthetic `synthetic-vulnerable-npm` package and
a synthetic advisory — **no real registry and no real OSV feed**.

It seeds the raw collector facts via a tier-1 SQL seed
(`scripts/seed-image-identity-facts.sql`, modeled on
`tests/fixtures/tfstate_drift/seed.sql`) so the three reducer domains can join
them:

| Seeded (raw collector facts) | Reducer-derived at runtime (not seeded) |
| --- | --- |
| `oci_registry.image_manifest` (digest identity) | `reducer_container_image_identity` (`exact_digest`, `image_ref`) |
| `sbom.document` + `sbom.component` | `reducer_sbom_attestation_attachment` (subject digest matched) |
| `attestation.statement` (subject binding) | `reducer_supply_chain_impact_finding` (`image_ref` populated) |
| `vulnerability.cve` + `vulnerability.affected_package` (synthetic advisory) | |

The corpus K8s Deployment references the **same** digest
(`demo.invalid/vuln-demo-app@sha256:1111…1111`), which is what lets the
image-identity reducer classify the digest as `exact_digest` and emit
`image_ref`. The seed itself contains no secrets: every value is synthetic and
public (`demo.invalid` is RFC 6761 reserved, the digest is an obvious
placeholder, the advisory exists on no feed).

```bash
ESHU_SRC=/path/to/eshu examples/supply-chain-demo/scripts/run-image-identity-proof.sh
```

The script asserts a finding whose `image_ref` and `subject_digest` equal the
seeded image identity, then prints PASS with the evidence path.

A daemon-free Go test proves the same reducer join logic without a stack:

```bash
cd go && go test ./internal/reducer -run TestBuildSupplyChainImpactFindingsDemoImageIdentityHop -count=1
```

It feeds the exact reducer correlation facts the demo produces and asserts the
finding carries `image_ref=demo.invalid/vuln-demo-app@sha256:1111…1111`,
`impact_status=affected_derived`, `match_reason=sbom_component_path`, and the
`sbom.component -> reducer_sbom_attestation_attachment ->
reducer_container_image_identity` evidence path.

## Not covered here

- The 10–15 minute screen recording is a manual deliverable.
- A **real** OCI-registry / OSV collector run for the image-identity hop: the
  OCI-registry collector is registry/ECR-gated and the synthetic advisory is on
  no feed, so `run-image-identity-proof.sh` **seeds** those facts rather than
  collecting them. The reducer join that produces `image_ref` is the real,
  unmocked path.

## Related docs

- [Supply-Chain Traceability](../../docs/public/supply-chain-traceability.md)
- [Supply-Chain Demo runbook (docs site)](../../docs/public/guides/supply-chain-demo.md)
- [SARIF Export](../../docs/public/reference/sarif-export.md)
- [Vulnerability Scanner Read Contract](../../docs/public/reference/vulnerability-scanner-read-contract.md)
- [Value-Flow Emission](../../docs/public/reference/value-flow-emission.md)
