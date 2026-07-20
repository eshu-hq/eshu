# Supply-Chain Traceability

Eshu traces a vulnerability from a published CVE all the way to a running
workload, and refuses to publish an impact finding until it owns the evidence
that connects them. This page is the entry point for that capability: the
chain, the surfaces that expose it, and the honest line between what is
production-promoted and what is gated or on the roadmap.

The chain Eshu models, end to end:

```
CVE
  -> advisory (CISA KEV, FIRST EPSS, OSV, NVD, GitLab/Gemnasium)
  -> package (npm, PyPI, Go, Maven, NuGet, Composer, RubyGems, Cargo,
              Swift, Pub, Hex, and OS apk/dpkg/rpm)
  -> manifest -> lockfile
  -> registry metadata
  -> container registry (ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog)
  -> image identity (digest-first)
  -> SBOM (CycloneDX, SPDX, in-toto, OCI referrers)
  -> deployment -> workload
  -> impact finding
```

## The Differentiated Claims

Three behaviors separate Eshu from a scanner that prints whatever a feed says.

1. **Owned evidence is required.** The reducer refuses to publish an impact
   finding from CVSS, EPSS, KEV, or a product-only CPE alone. An advisory only
   becomes a finding when it joins owned package, manifest, lockfile, SBOM,
   image, workload, or service evidence. The
   [Vulnerability Scanner Read Contract](reference/vulnerability-scanner-read-contract.md)
   lets a client ask, before querying findings, exactly what Eshu will and will
   not publish.
2. **SBOM-to-image attachment requires subject-digest evidence**, not SBOM
   presence. An SBOM that does not carry a subject digest matching an owned
   image identity does not attach.
3. **Reachability is per-ecosystem and honest about gating.** Go reachability
   runs through govulncheck and is always on. Python, TypeScript, and
   JavaScript value-flow reachability is opt-in behind the
   [`ESHU_EMIT_DATAFLOW` gate](reference/value-flow-emission.md). JVM
   reachability is bounded. See the per-ecosystem support detail in the
   [Vulnerability Scanner Confidence Matrix](reference/vulnerability-scanner-confidence.md).

## Surfaces

| Surface | What it does | Reference |
| --- | --- | --- |
| Advisory sources | Collect CISA KEV, FIRST EPSS, OSV, NVD source facts with provenance and freshness | [Security Intelligence](reference/security-intelligence.md) |
| GitLab Gemnasium | Normalize GitLab Advisory Database records into canonical ecosystems and the shared impact reducer | [GitLab Gemnasium Integration](reference/gitlab-gemnasium-integration.md) |
| Read contract | Tell a client what is admitted, refused, or partial before it queries findings | [Vulnerability Scanner Read Contract](reference/vulnerability-scanner-read-contract.md) |
| Impact findings | Reducer-owned findings with impact status, reachability, advisory sources, suppression | [MCP Reference](reference/mcp-reference.md) |
| Secret scanning | Investigate hardcoded secrets with redacted findings and suppression evidence | [Hardcoded Secrets Investigation](reference/hardcoded-secrets-investigation.md) |
| SARIF export | Export findings as SARIF v2.1.0 for GitHub Code Scanning, GitLab, IDEs, and SIEMs | [SARIF Export](reference/sarif-export.md) |
| Value-flow / taint | Opt-in per-function value-flow and taint emission behind a runtime gate | [Value-Flow Emission](reference/value-flow-emission.md) |
| Readiness ledger | Per-ecosystem and per-target-family confidence | [Vulnerability Scanner Confidence Matrix](reference/vulnerability-scanner-confidence.md) |

## Cloud Posture: AWS-First

Eshu's cloud posture surface is production-promoted for AWS today. Only
`aws_resource_materialization` is promoted to a versioned, hashed
`cloud_resource_node` conflict family. GCP, Azure, EC2-instance, Kubernetes,
and security-group node materializers remain risky resource-scope fallbacks
until partition-filtered handler proof exists, as recorded in
[Collector And Reducer Readiness](reference/collector-reducer-readiness.md).

Launch copy must say this plainly: **AWS is the production-promoted cloud
posture surface. Azure, GCP, and live Kubernetes posture are on the public
[roadmap](roadmap.md).** A multi-cloud claim without that caveat is a
credibility risk for a GCP-first or Azure-first buyer.

## What Is Gated Or Preview

Cloud posture state uses the canonical readiness lanes from
[Collector And Reducer Readiness](reference/collector-reducer-readiness.md#readiness-vocabulary);
reachability rows are per-ecosystem capability states.

| Capability | State | Gate / condition |
| --- | --- | --- |
| Go reachability (govulncheck) | Always on | None |
| Python / TypeScript / JavaScript value-flow reachability | Preview, opt-in | [`ESHU_EMIT_DATAFLOW`](reference/value-flow-emission.md) |
| JVM reachability | Bounded | Own reducer family, partial coverage |
| AWS cloud posture | `implemented` (production-promoted) | None |
| GCP / Azure cloud posture | `gated` | See [roadmap](roadmap.md#promotion-readiness) |
| Kubernetes-live cloud posture | `foundation_only` | See [roadmap](roadmap.md#promotion-readiness) |
| SLSA provenance attestation facts (`attestation.slsa_provenance`) | `typed, not emitted` | The payload schema exists but no collector emits this kind today. See [issue #5371](https://github.com/eshu-hq/eshu/issues/5371). |

## Running The Chain End To End

The [Supply-Chain Demo Runbook](guides/supply-chain-demo.md) is the runnable
CVE-to-impact demo (issue
[#3019](https://github.com/eshu-hq/eshu/issues/3019)). It walks the chain from a
clean clone using the synthetic corpus in
[`examples/supply-chain-demo/`](https://github.com/eshu-hq/eshu/tree/main/examples/supply-chain-demo):
a fixture repository with a deterministic vulnerable dependency, the refusal path
when owned advisory evidence is missing, and — with the Compose stack and seeded
advisory facts — the full image build, SBOM attachment, and a Claude Code session
that returns the chain. The runbook is explicit about which steps run fully
offline and which require the stack.

## Related

- [Security Intelligence](reference/security-intelligence.md)
- [Vulnerability Scanner Confidence Matrix](reference/vulnerability-scanner-confidence.md)
- [Collector And Reducer Readiness](reference/collector-reducer-readiness.md)
- [Roadmap](roadmap.md)
