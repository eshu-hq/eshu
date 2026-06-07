// console/demoModel.ts
// Neutral demo fixture (generic sample workspace — no company data) used ONLY as
// a test fixture for page components. It is NOT a runtime data source: the app
// renders live API data exclusively (see App.tsx and console/liveModel.ts).

import type {
  ConsoleModel, GraphModel, RelationshipRow, SeriesBundle
} from "./types";
import { uiTruth, uiFresh } from "./types";
import { modelFromSnapshot } from "./liveModel";

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
  graphNodes: ramp(44, 48, 41000, 400, 240),
  graphEdges: ramp(55, 48, 128000, 1200, 900),
  queryP99: ramp(99, 48, 28, 12, 0),
  newVulns: ramp(123, 14, 3, 5, -0.1)
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
    queueOutstanding: 12, inFlight: 2, deadLetters: 0, succeeded: 41280, profile: "local_full_stack"
  },
  services: [
    { id: "checkout-service", name: "checkout-service", kind: "service", repo: "sample/checkout-service", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh" },
    { id: "payments-api", name: "payments-api", kind: "service", repo: "sample/payments-api", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh" },
    { id: "ledger-service", name: "ledger-service", kind: "service", repo: "sample/ledger-service", environments: ["prod-us-east-1"], truth: "exact", freshness: "fresh" },
    { id: "lib-common", name: "lib-common", kind: "library", repo: "sample/lib-common", environments: [], truth: "exact", freshness: "fresh" }
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
  images: [
    {
      id: "oci-image://registry.example/sample/checkout@sha256:abc123", digest: "sha256:abc1234567890def",
      repositoryId: "oci-registry://registry.example/sample/checkout", registry: "registry.example",
      repository: "sample/checkout", name: "checkout", tag: "1.4.2",
      mediaType: "application/vnd.oci.image.manifest.v1+json", artifactType: "",
      configDigest: "sha256:cfg9876543210", sizeBytes: 28475610, sourceSystem: "oci_registry"
    }
  ],
  truth: {},
  provenance: { runtime: "live", services: "live", findings: "live", vulnerabilities: "live", sbom: "live", images: "live" },
  graph: demoGraph,
  relationships: demoRelationships,
  series: demoSeries
};

export { uiTruth, uiFresh };
// Re-exported for any caller that imported it here historically; the live-only
// implementation now lives in console/liveModel.ts.
export { modelFromSnapshot };
