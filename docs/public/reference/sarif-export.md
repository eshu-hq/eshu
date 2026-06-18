# SARIF Export

Eshu can render vulnerability scanner findings as SARIF v2.1.0 (Static Analysis
Results Interchange Format). SARIF is the interchange format consumed by GitHub
Code Scanning, GitLab, IDE SARIF viewers, and many SIEMs, so a SARIF export lets
Eshu findings flow into the security tooling teams already run instead of staying
behind an Eshu-specific surface.

The exporter emits SARIF schema `https://json.schemastore.org/sarif-2.1.0.json`
with the `version` field set to `2.1.0`. Output is pretty-printed with a fixed
two-space indent and a single trailing newline so the bytes are stable across
runs.

## What is exported

The SARIF export consumes the **vulnerability scan finding set** produced by
`eshu vuln-scan repo` — the reducer-owned vulnerability impact findings for one
repository. Each SARIF `result` corresponds to one vulnerability finding and
carries:

- Advisory identity (advisory id, CVE id) and package identity (package id,
  package name, PURL, ecosystem, observed version).
- Severity, CVSS score and vector, EPSS probability, and known-exploited (KEV)
  status.
- Reachability enrichment (state, confidence, source, evidence, reason, and
  missing-evidence notes), impact status, and confidence.
- Remediation metadata (fixed version, vulnerable range, first-patched version,
  manifest fix range, and whether the manifest allows the fix).
- Supply-chain context: dependency scope, dependency path, direct/indirect flag,
  and the workload, service, and environment ids the finding touches.
- Manifest locations (path plus optional line region).

Run-level scanner readiness (readiness state, freshness, exit code/reason, scope
mode, missing evidence, incomplete reasons, and unsupported targets) is attached
to the SARIF run properties.

### Honest scope

- This export covers **vulnerability / supply-chain impact findings only**. The
  exporter input is the `Finding` set, whose fields are vulnerability- and
  package-oriented (advisory, CVE, PURL, severity, reachability, remediation).
  There is no field for source-code secret matches, so **hardcoded-secrets
  findings are not part of this export path**.
- It is a **CLI export, not an MCP tool**. SARIF is produced by the
  `eshu vuln-scan repo` command writing to stdout. There is no MCP or HTTP API
  tool that returns SARIF.
- The export is **scoped to a single repository**. Findings whose own
  repository id disagrees with the scoped repository are dropped (the count is
  reported in the run properties), so a single SARIF run never mixes evidence
  from two targets.
- Empty optional fields are omitted rather than filled with invented values. A
  finding that carries no manifest location produces no `locations` block; a
  finding with no advisory metadata produces no vendor properties.

## How to invoke

SARIF is selected with the `--export sarif` flag on `eshu vuln-scan repo`:

```bash
eshu vuln-scan repo ./path/to/repo --export sarif > findings.sarif.json
```

Notes:

- The supported `--export` values are `sarif` and `vex`. Any other value is
  rejected.
- `--export` cannot be combined with `--json`; the two are different output
  contracts. When an export format is set, the human scan summary is written to
  stderr and the export document is written to stdout.
- The scan must reach a ready state before findings can be read. If the target
  is not ready, rerun with `--wait=true` first.

## Example output

The snippet below is **abridged and illustrative** (package names, identifiers,
and versions are placeholders). A real document contains the full rule and
result property sets described in the mapping table.

```json
{
  "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "eshu",
          "version": "x.y.z",
          "informationUri": "https://eshu.dev",
          "rules": [
            {
              "id": "ADVISORY-aaaa-bbbb-cccc",
              "name": "example-package",
              "shortDescription": { "text": "Prototype pollution in example-package" },
              "fullDescription": { "text": "Versions before 4.17.21 allow prototype pollution." },
              "helpUri": "https://example.test/advisories/ADVISORY-aaaa-bbbb-cccc",
              "defaultConfiguration": { "level": "error" },
              "properties": {
                "eshu.severity": "high",
                "eshu.cvssScore": 7.4,
                "eshu.cvssVector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:L",
                "eshu.epssProbability": "0.12345",
                "eshu.ecosystem": "example-ecosystem",
                "eshu.purl": "pkg:example/example-package@4.17.20",
                "tags": ["ecosystem:example-ecosystem", "security", "vulnerability"]
              }
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "ADVISORY-aaaa-bbbb-cccc",
          "ruleIndex": 0,
          "level": "error",
          "message": {
            "text": "ADVISORY-aaaa-bbbb-cccc in example-package@4.17.20; fixed in 4.17.21 — Prototype pollution in example-package"
          },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": { "uri": "manifest.example" },
                "region": { "startLine": 12, "endLine": 12 }
              }
            }
          ],
          "partialFingerprints": {
            "eshu/advisoryId/v1": "ADVISORY-aaaa-bbbb-cccc",
            "eshu/cveId/v1": "CVE-0000-0000",
            "eshu/findingId/v1": "fnd-001",
            "eshu/observedVersion/v1": "4.17.20",
            "eshu/packageId/v1": "pkg-example-package"
          },
          "properties": {
            "eshu.findingId": "fnd-001",
            "eshu.packageId": "pkg-example-package",
            "eshu.packageName": "example-package",
            "eshu.observedVersion": "4.17.20",
            "eshu.fixedVersion": "4.17.21",
            "eshu.repositoryId": "repo-example"
          }
        }
      ],
      "properties": {
        "eshu.scope": "repository:repo-example",
        "eshu.generatedAt": "2026-01-01T00:00:00Z",
        "eshu.formatVersion": "2.1.0"
      }
    }
  ]
}
```

## Finding-to-SARIF mapping

The exporter groups findings into SARIF rules keyed by advisory identity (one
rule per distinct advisory/CVE), and emits one result per finding. All
Eshu-specific fields are written under a vendor `eshu.` prefix so they never
collide with reserved SARIF property names.

| Eshu finding field | SARIF location |
| --- | --- |
| `AdvisoryID` (else `CVEID`, else `FindingID`) | `result.ruleId` and `rule.id` |
| `Severity` | `result.level` and `rule.defaultConfiguration.level` |
| `Summary` | `rule.shortDescription.text` and appended to `result.message.text` |
| `Description` | `rule.fullDescription.text` |
| `PackageName` (else `PURL`, else rule id) | `rule.name` |
| `HelpURI` | `rule.helpUri` |
| `Severity` (raw) | `rule.properties["eshu.severity"]` |
| `CVSSScore` | `rule.properties["eshu.cvssScore"]` |
| `CVSSVector` | `rule.properties["eshu.cvssVector"]` |
| `EPSSProbability` | `rule.properties["eshu.epssProbability"]` |
| `KnownExploited` | `rule.properties["eshu.knownExploited"]` |
| `Ecosystem` | `rule.properties["eshu.ecosystem"]` |
| `PURL` | `rule.properties["eshu.purl"]` |
| `AdvisorySources` | `rule.properties["eshu.advisorySources"]` |
| (derived) | `rule.properties["tags"]` (`security`, `vulnerability`, plus `kev` and `ecosystem:<name>` when present) |
| `Locations[].ManifestPath` | `result.locations[].physicalLocation.artifactLocation.uri` |
| `Locations[].StartLine` / `EndLine` | `result.locations[].physicalLocation.region.startLine` / `endLine` |
| `FindingID` | `result.partialFingerprints["eshu/findingId/v1"]` |
| `CVEID` | `result.partialFingerprints["eshu/cveId/v1"]` |
| `AdvisoryID` | `result.partialFingerprints["eshu/advisoryId/v1"]` |
| `PackageID` | `result.partialFingerprints["eshu/packageId/v1"]` |
| `ObservedVersion` | `result.partialFingerprints["eshu/observedVersion/v1"]` |
| `PackageID`, `PackageName`, `ObservedVersion`, `FixedVersion`, `RepositoryID`, `SubjectDigest`, `ImageRef` | `result.properties["eshu.*"]` |
| `Reachability.*`, `RuntimeReachability`, `ImpactStatus`, `Confidence`, `MatchReason`, `RequestedRange`, `VulnerableRange` | `result.properties["eshu.*"]` |
| `WorkloadIDs`, `ServiceIDs`, `Environments`, `DependencyScope`, `DependencyPath`, `DirectDependency` | `result.properties["eshu.*"]` |
| `MissingEvidence`, `EvidenceFactIDs`, `SourceFreshness` | `result.properties["eshu.*"]` |
| `Remediation.*` | `result.properties["eshu.remediation"]` |
| Snapshot timestamp | `run.invocations[].startTimeUtc` / `endTimeUtc` and `run.properties["eshu.generatedAt"]` |
| Snapshot scope and status | `run.properties["eshu.scope"]`, `run.properties["eshu.formatVersion"]`, and the scanner readiness fields under `run.properties["eshu.*"]` |

Severity maps to the SARIF `level` vocabulary as follows:

| Eshu severity | SARIF level |
| --- | --- |
| `critical` | `error` |
| `high` | `error` |
| `medium` | `warning` |
| `low` | `note` |
| `none` (and unknown) | `none` |

The `partialFingerprints` keys are versioned (`eshu/...​/v1`) so downstream
result-tracking systems (for example GitHub Code Scanning alert dedupe) can
correlate the same finding across runs even when its physical location moves.

## Integration examples

The export is a standard SARIF v2.1.0 document, so any consumer that accepts
SARIF can ingest it. Conceptual steps for common targets:

- **GitHub Code Scanning** — produce the SARIF file in CI, then upload it to the
  code-scanning SARIF ingestion endpoint (for example via the code-scanning
  upload action or API). GitHub renders each result as a code-scanning alert and
  uses `partialFingerprints` for alert continuity across runs.
- **GitLab** — publish the SARIF file as a job artifact in the format GitLab
  expects for security reports so it appears in the project's security
  dashboard.
- **VS Code SARIF Viewer** — open the generated `.sarif`/`.sarif.json` file in
  the SARIF Viewer extension to browse rules, results, and the manifest
  locations inline.

The exact CI wiring depends on your platform and runner; the common requirement
is only that the SARIF document is generated as a file and handed to the
consumer's SARIF ingestion path.

## Determinism and redaction

- **Deterministic output.** Findings, rules, locations, advisory sources, and
  tags are sorted before serialization, and the JSON is emitted with a fixed
  indent and trailing newline, so the same snapshot always produces
  byte-identical output. This is locked by golden fixtures and a determinism
  test (`TestSARIFExporter_IsDeterministic`).
- **Path redaction.** When a redactor is configured, every manifest path is
  passed through it before it is written to `artifactLocation.uri`, and the
  number of redacted paths is reported in `run.properties["eshu.redactedPaths"]`.
  This is covered by `TestSARIFExporter_AppliesPathRedaction`. Findings dropped
  for being out of scope are counted in `run.properties["eshu.droppedFindings"]`.

## Related

- [Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
- [CLI Reference](cli-reference.md)
- [Security Intelligence](security-intelligence.md)
- [Supply Chain Traceability](../supply-chain-traceability.md)
- [Supply-Chain Demo Runbook](../guides/supply-chain-demo.md)
