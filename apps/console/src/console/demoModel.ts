// console/demoModel.ts
// Neutral prospect demo fixture (generic sample workspace — no company data).
// App demo mode renders this explicit fixture source; private mode still renders
// only the Eshu API and never falls back to these rows.

import { modelFromSnapshot } from "./liveModel";
import type {
  ConsoleModel, GraphModel, RelationshipRow, SeriesBundle
} from "./types";
import { uiTruth, uiFresh } from "./types";

function ramp(seed: number, n: number, base: number, amp: number, drift: number): number[] {
  let s = seed | 0; const out: number[] = []; let v = base;
  for (let i = 0; i < n; i++) {
    s = (s + 0x6d2b79f5) | 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    const r = ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    v += (r - 0.5) * amp + drift;
    out.push(Math.max(0, Math.round(v)));
  }
  return out;
}

const demoSeries: SeriesBundle = {
  ingestRate: ramp(11, 48, 620, 200, 1),
  queueDepth: ramp(22, 48, 180, 110, -2),
  deadLetters: ramp(33, 48, 3, 4, -0.05),
  graphNodes: ramp(44, 48, 41000, 400, 240),
  graphEdges: ramp(55, 48, 128000, 1200, 900),
  queryP50: ramp(77, 48, 4, 2, 0),
  queryP95: ramp(88, 48, 12, 5, 0),
  queryP99: ramp(99, 48, 28, 12, 0),
  newVulns: ramp(123, 14, 3, 5, -0.1),
  metricsConfigured: true
};

const demoGraph: GraphModel = {
  nodes: [
    { id: "repo:checkout", kind: "repo", label: "checkout-service", sub: "sample/checkout", col: 0 },
    { id: "img:checkout", kind: "image", label: "checkout:1.4.2", sub: "registry", col: 1 },
    { id: "svc:checkout", kind: "service", label: "checkout-service", sub: "tier-1 · Payments", col: 2, hero: true, truth: "exact" },
    { id: "svc:payments", kind: "service", label: "payments-api", sub: "tier-1", col: 2, truth: "exact" },
    { id: "svc:ledger", kind: "service", label: "ledger-service", sub: "tier-1", col: 3, truth: "exact" },
    { id: "wl:checkout", kind: "workload", label: "Deployment/checkout", sub: "3 replicas", col: 3 },
    { id: "env:prod", kind: "env", label: "prod-us-east-1", sub: "Kubernetes", col: 4 },
    { id: "ds:db", kind: "datastore", label: "checkout-db", sub: "PostgreSQL", col: 4 },
    { id: "vuln:x", kind: "vuln", label: "CVE-2024-0001", sub: "CVSS 8.1", col: 2, truth: "inferred" }
  ],
  edges: [
    { s: "repo:checkout", t: "img:checkout", verb: "BUILDS", layer: "deploy" },
    { s: "img:checkout", t: "svc:checkout", verb: "DEPLOYS_FROM", layer: "deploy" },
    { s: "svc:checkout", t: "svc:payments", verb: "DEPENDS_ON", layer: "runtime" },
    { s: "svc:payments", t: "svc:ledger", verb: "DEPENDS_ON", layer: "runtime" },
    { s: "svc:checkout", t: "wl:checkout", verb: "RUNS_AS", layer: "runtime" },
    { s: "wl:checkout", t: "env:prod", verb: "RUNS_IN", layer: "runtime" },
    { s: "svc:checkout", t: "ds:db", verb: "STORES_IN", layer: "infra" },
    { s: "img:checkout", t: "vuln:x", verb: "AFFECTED_BY", layer: "security" }
  ]
};

const demoRelationships: readonly RelationshipRow[] = [
  { verb: "IMPORTS", layer: "code", count: 1840, detail: "Module import edges" },
  { verb: "CALLS", layer: "code", count: 932, detail: "Symbol call edges" },
  { verb: "DEPLOYS_FROM", layer: "deploy", count: 28, detail: "Workload built from a repo" },
  { verb: "DECLARED_BY", layer: "infra", count: 410, detail: "Resource declared by Terraform" },
  { verb: "RUNS_IN", layer: "runtime", count: 46, detail: "Workload placed in an environment" },
  { verb: "DEPENDS_ON", layer: "runtime", count: 88, detail: "Runtime dependency" },
  { verb: "AFFECTED_BY", layer: "security", count: 34, detail: "Component affected by a vulnerability" }
];

export const demoModel: ConsoleModel = {
  source: "demo",
  runtime: {
    indexStatus: "complete", repositories: 6, workloads: 9, platforms: 2, instances: 14,
    queueOutstanding: 12, inFlight: 2, deadLetters: 0, succeeded: 41280, profile: "demo_fixture"
  },
  services: [
    { id: "checkout-service", name: "checkout-service", kind: "service", repo: "sample/checkout-service", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
    { id: "payments-api", name: "payments-api", kind: "service", repo: "sample/payments-api", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
    { id: "ledger-service", name: "ledger-service", kind: "service", repo: "sample/ledger-service", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh", tier: "tier-2", category: "service", domain: "finance", language: "Go" },
    { id: "lib-common", name: "lib-common", kind: "library", repo: "sample/lib-common", environments: [], truth: "exact", freshness: "fresh", tier: "library", category: "library", domain: "core-engineering", language: "TypeScript" }
  ],
  languages: [
    { language: "TypeScript", count: 3 }, { language: "Go", count: 2 }, { language: "Python", count: 1 }
  ],
  ingesters: [
    { id: "git-primary", kind: "git", state: "healthy", facts: 41280, freshness: "fresh" },
    { id: "k8s-observer", kind: "kubernetes", state: "healthy", facts: 12040, freshness: "fresh" },
    { id: "vuln-intel", kind: "vulnerability_intelligence", state: "healthy", facts: 8810, freshness: "fresh" }
  ],
  findings: [
    { id: "d1", type: "Vulnerability", entity: "checkout-service", title: "CVE-2024-0001 reachable in prod image", detail: "checkout:1.4.2 ships an affected dependency.", truth: "fallback" },
    { id: "d2", type: "Dead code", entity: "checkout-service", title: "Unreferenced symbol legacyDiscount", detail: "src/discounts.ts · no callers", truth: "derived" }
  ],
  vulnerabilities: [
    { id: "CVE-2024-0001", package: "sample-lib", severity: "high", cvss: 8.1, kev: false, fixedVersion: "2.0.1", services: ["checkout-service"] }
  ],
  sbom: { total: 3, verified: 1, sbomCount: 2, attestationCount: 1 },
  dependencies: [
    { direction: "forward", anchorPackage: "sample-lib", anchorPackageId: "npm://sample-lib", declaringVersion: "1.0.0", relatedPackage: "left-pad", relatedPackageId: "npm://left-pad", ecosystem: "npm", range: "^1.3.0", dependencyType: "runtime", optional: false, edgeId: "dep-1" }
  ],
  images: [
    {
      id: "oci-image://registry.example/sample/checkout@sha256:abc123", digest: "sha256:abc1234567890def",
      repositoryId: "oci-registry://registry.example/sample/checkout", registry: "registry.example",
      repository: "sample/checkout", name: "checkout", tag: "1.4.2",
      mediaType: "application/vnd.oci.image.manifest.v1+json", artifactType: "",
      configDigest: "sha256:cfg9876543210", sizeBytes: 28475610, sourceSystem: "oci_registry"
    }
  ],
  iacResources: [
    {
      category: "iam",
      id: "tf-demo-1",
      kind: "resource",
      lineNumber: 12,
      module: "checkout",
      name: "module.\"checkout\".aws_iam_role.this",
      provider: "aws",
      relativePath: "iam.tf",
      repoId: "checkout-service",
      resourceName: "this",
      service: "aws.iam",
      type: "aws_iam_role"
    },
    {
      category: "storage",
      id: "tf-demo-2",
      kind: "resource",
      lineNumber: 8,
      module: "",
      name: "aws_s3_bucket.assets",
      provider: "aws",
      relativePath: "storage.tf",
      repoId: "checkout-service",
      resourceName: "assets",
      service: "aws.s3",
      type: "aws_s3_bucket"
    }
  ],
  advisories: [
    { id: "CVE-2021-44228", cveId: "CVE-2021-44228", ghsaId: "GHSA-jfh8-c2jp-5v3q", severity: "critical", cvss: 10, kev: true, ecosystems: ["maven"], packageIds: ["pkg:maven/org.apache.logging.log4j/log4j-core"], publishedAt: "2021-12-10" }
  ],
  collectorReadiness: [
    {
      blockingGate: "none",
      claimDriven: false,
      claimState: "direct",
      displayName: "Git Repository",
      evidence: ["source facts", "reducer facts", "API/MCP evidence"],
      family: "Source collection",
      health: "healthy",
      instanceId: "git-primary",
      kind: "git",
      lastProof: "41280 observations",
      reducerReadback: "available",
      sourceScope: "repository",
      state: "implemented",
      stateLabel: "implemented"
    },
    {
      blockingGate: "claim-driven collector registered with claims disabled",
      claimDriven: true,
      claimState: "direct",
      displayName: "PagerDuty",
      evidence: ["API/MCP evidence"],
      family: "Operations evidence",
      health: "healthy",
      instanceId: "pagerduty-preview",
      kind: "pagerduty",
      lastProof: "not observed",
      reducerReadback: "unavailable",
      sourceScope: "pagerduty_account",
      state: "gated",
      stateLabel: "gated"
    },
    {
      blockingGate: "no configured instance for this collector family",
      claimDriven: true,
      claimState: "none",
      displayName: "Kubernetes Live",
      evidence: ["API/MCP evidence"],
      family: "Cloud and runtime",
      health: "unsupported",
      instanceId: "",
      kind: "kubernetes_live",
      lastProof: "not observed",
      reducerReadback: "unavailable",
      sourceScope: "cluster",
      state: "unsupported",
      stateLabel: "unsupported"
    }
  ],
  argoCDApps: [
    { id: "argo:checkout-app", name: "checkout-app", kind: "ArgoCDApplication", source: "sample/checkout-service", sourceIndexed: true },
    { id: "argo:payments-app", name: "payments-app", kind: "ArgoCDApplication", source: "sample/payments-api", sourceIndexed: true },
    { id: "argo:ledger-app", name: "ledger-app", kind: "ArgoCDApplication", source: "sample/ledger-service", sourceIndexed: true },
    { id: "argo:external-app", name: "external-app", kind: "ArgoCDApplication", source: "https://github.com/sample/external-configs", sourceIndexed: false }
  ],
  truth: {},
  provenance: {
    runtime: "demo",
    services: "demo",
    findings: "demo",
    vulnerabilities: "demo",
    sbom: "demo",
    dependencies: "demo",
    images: "demo",
    iacResources: "demo",
    advisories: "demo",
    collectorReadiness: "demo",
    argoCDApps: "demo"
  },
  graph: demoGraph,
  relationships: demoRelationships,
  series: demoSeries
};

export { uiTruth, uiFresh };
// Re-exported for any caller that imported it here historically; the live-only
// implementation now lives in console/liveModel.ts.
export { modelFromSnapshot };
