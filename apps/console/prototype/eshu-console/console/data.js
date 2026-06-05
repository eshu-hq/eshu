/* Eshu Console — sample dataset (demo fixtures).
   This is swappable demo content, NOT part of the product. Live mode reads the
   real org from the Eshu API; nothing here is hard-coded into the console UI.
   Provenance:  exact   = read directly from repo files (package.json, Dockerfile, argocd, catalog-info)
                derived = inferred from manifests / version ranges
                inferred= representative (runtime/incident/scan data Eshu would collect live)
   Exposes window.ESHU. Plain JS (no JSX). */
(function () {
  "use strict";

  function mulberry32(seed) {
    return function () {
      seed |= 0; seed = (seed + 0x6D2B79F5) | 0;
      let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }
  function series(seed, n, base, amp, drift) {
    const rnd = mulberry32(seed); const out = []; let v = base;
    for (let i = 0; i < n; i++) { v += (rnd() - 0.5) * amp + drift; out.push(Math.max(0, Math.round(v))); }
    return out;
  }
  function fseries(seed, n, base, amp, drift, dp) {
    const rnd = mulberry32(seed); const out = []; let v = base; const f = Math.pow(10, dp || 1);
    for (let i = 0; i < n; i++) { v += (rnd() - 0.5) * amp + drift; out.push(Math.max(0, Math.round(Math.max(0, v) * f) / f)); }
    return out;
  }

  const ENV = ["bg-prod", "bg-qa", "ops-qa"];

  const lang = {
    ts: { label: "TypeScript", color: "#3b82f6" },
    js: { label: "JavaScript", color: "#f5b73d" },
    go: { label: "Go", color: "#14b8a6" },
    py: { label: "Python", color: "#a78bfa" },
    hcl: { label: "Terraform", color: "#8b5cf6" }
  };

  const collectorKinds = {
    git: { label: "Git", color: "#f3ebdd", glyph: "git" },
    aws: { label: "AWS Cloud", color: "#ff9d2e", glyph: "aws" },
    terraform_state: { label: "Terraform State", color: "#8b5cf6", glyph: "tf" },
    oci_registry: { label: "ECR Registry", color: "#22d3ee", glyph: "oci" },
    kubernetes: { label: "Kubernetes (EKS)", color: "#4f8cff", glyph: "k8s" },
    vulnerability_intelligence: { label: "Vuln Intelligence", color: "#f0506e", glyph: "vuln" },
    security_alert: { label: "Dependabot Alerts", color: "#fb7185", glyph: "shield" },
    pagerduty: { label: "PagerDuty", color: "#22c55e", glyph: "pd" },
    jira: { label: "Jira", color: "#4f8cff", glyph: "jira" },
    package_registry: { label: "npm Registry", color: "#f0506e", glyph: "doc" },
    prometheus_mimir: { label: "Prometheus", color: "#ff8a00", glyph: "prom" },
    sbom_attestation: { label: "SBOM", color: "#2dd4bf", glyph: "sbom" }
  };

  // collectors framed against the real boatsgroup stack
  const collectors = [
    { kind: "git", instance: "git-multi-host", status: "degraded", facts: 142880, scopes: 34, lastRun: "1m ago", latencyMs: 410, freshness: "lagging", cadence: "webhook + 10m poll", note: "34 repos across Bitbucket, GitHub Enterprise & github.com" },
    { kind: "kubernetes", instance: "eks-observer", status: "healthy", facts: 61240, scopes: 3, lastRun: "40s ago", latencyMs: 360, freshness: "fresh", cadence: "watch", note: "bg-prod, bg-qa, ops-qa · ArgoCD-synced workloads" },
    { kind: "aws", instance: "aws-bg", status: "healthy", facts: 88410, scopes: 2, lastRun: "3m ago", latencyMs: 1720, freshness: "fresh", cadence: "30m claim", note: "IRSA, SecretsManager, Route53, API Gateway, ElastiCache, ACM" },
    { kind: "terraform_state", instance: "tfstate-bg", status: "healthy", facts: 33120, scopes: 12, lastRun: "8m ago", latencyMs: 940, freshness: "fresh", cadence: "on-apply + 1h", note: "helm-charts/shared + terraform-stack-* + iac-eks-*" },
    { kind: "oci_registry", instance: "ecr-bg", status: "healthy", facts: 18760, scopes: 41, lastRun: "2m ago", latencyMs: 520, freshness: "fresh", cadence: "5m poll", note: "ECR · node-api-base:1.0.0 + 40 service images" },
    { kind: "package_registry", instance: "npm-dmm", status: "healthy", facts: 24910, scopes: 34, lastRun: "4m ago", latencyMs: 600, freshness: "fresh", cadence: "on-publish", note: "@dmm/* internal scope · client + lib packages" },
    { kind: "vulnerability_intelligence", instance: "vuln-intel", status: "healthy", facts: 41203, scopes: 9, lastRun: "6m ago", latencyMs: 1310, freshness: "fresh", cadence: "15m claim", note: "CISA KEV · EPSS · NVD · OSV · GHSA" },
    { kind: "security_alert", instance: "dependabot", status: "healthy", facts: 6120, scopes: 34, lastRun: "5m ago", latencyMs: 430, freshness: "fresh", cadence: "10m poll", note: "GitHub Dependabot repository alerts" },
    { kind: "jira", instance: "jira-dmm", status: "healthy", facts: 12840, scopes: 6, lastRun: "7m ago", latencyMs: 720, freshness: "fresh", cadence: "webhook + 15m", note: "DMM-NODE · work-item correlation" },
    { kind: "pagerduty", instance: "pd-prod", status: "degraded", facts: 2410, scopes: 8, lastRun: "21m ago", latencyMs: 4200, freshness: "lagging", cadence: "webhook + 15m", note: "Rate-limited by provider (429) — backing off" },
    { kind: "prometheus_mimir", instance: "mimir-bg", status: "healthy", facts: 21870, scopes: 3, lastRun: "30s ago", latencyMs: 230, freshness: "fresh", cadence: "1m poll", note: "Grafana dashboards shipped per ArgoCD app" },
    { kind: "sbom_attestation", instance: "sbom-attest", status: "stale", facts: 3110, scopes: 6, lastRun: "5h 12m ago", latencyMs: 0, freshness: "stale", cadence: "on-publish", note: "Few service images carry a cosign SBOM referrer" }
  ];

  // ------------------------------------------------------------- services
  // kind: api | lib | web | job ;  tier reflects criticality
  const services = [
    { id: "api-node-boats", name: "api-node-boats", kind: "api", repo: "boats-group/api-node-boats", host: "bitbucket", version: "4.3.1", lang: "ts", tier: "tier-1", owner: "core-engineering", system: "Boat-Search", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-boats:4.3.1", port: 3081, deps: ["api-node-forex", "lib-api-hapi", "lib-common", "lib-logging", "dmm-clients"], stores: ["Elasticsearch", "ElastiCache (Memcached)"], crit: 1, high: 3, med: 6, low: 11, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.96, calls: 34, callers: 12, blastRadius: 18, story: "api-node-boats is the boat-search API (Hapi/TypeScript, v4.3.1) owned by core-engineering in the Boat-Search system. It reads from Elasticsearch and caches in ElastiCache, builds on node:20-alpine → node-api-base:1.0.0, and deploys via ArgoCD/Kustomize to the api-node namespace on EKS (bg-prod, bg-qa). Its published @dmm/api-node-boats-client is consumed by platform, conversation, boattrader and fsbo." },
    { id: "api-node-platform", name: "api-node-platform", kind: "api", repo: "DMM-NODE/api-node-platform", host: "ghe", version: "10.3.2", lang: "ts", tier: "tier-1", owner: "platform", system: "Platform", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-platform:10.3.2", port: 3081, deps: ["api-node-boats", "api-node-conversation", "api-node-forex", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "dmm-clients"], stores: [], crit: 2, high: 5, med: 9, low: 17, incidents: 0, workItems: 8, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 61, callers: 6, blastRadius: 22, story: "api-node-platform (v10.3.2) is the orchestrator API. It composes a dozen internal clients — boats, conversation, editorial, forex, spam-fraud, user-management, yw-fsbo, ai-provider — and is the widest internal consumer of api-node-boats (pinned at ^3.5.0)." },
    { id: "api-node-conversation", name: "api-node-conversation", kind: "api", repo: "boatsgroup/api-node-conversation", host: "github", version: "1.3.0", lang: "ts", tier: "tier-2", owner: "messaging", system: "Messaging", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-conversation:1.3.0", port: 3081, deps: ["api-node-boats", "lib-api-hapi", "lib-logging"], stores: ["PostgreSQL (drizzle)"], crit: 0, high: 2, med: 5, low: 8, incidents: 1, workItems: 3, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 18, callers: 3, blastRadius: 7, story: "api-node-conversation manages SMS/WhatsApp threads via Twilio, persisting to PostgreSQL through drizzle-orm. It consumes api-node-boats-client ^3.21.0 and is consumed in turn by platform and boattrader." },
    { id: "api-node-boattrader", name: "api-node-boattrader", kind: "api", repo: "boats-group/api-node-boattrader", host: "bitbucket", version: "1.3.0", lang: "ts", tier: "tier-1", owner: "marketplace", system: "Marketplace", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-boattrader:1.3.0", port: 3081, deps: ["api-node-boats", "api-node-conversation", "api-node-forex", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "dmm-clients"], stores: ["Valkey"], crit: 1, high: 4, med: 7, low: 13, incidents: 0, workItems: 5, freshness: "fresh", truth: "exact", coverage: 0.91, calls: 44, callers: 2, blastRadius: 6, story: "api-node-boattrader powers the Boat Trader web & mobile apps. It caches in Valkey, consumes api-node-boats-client ^3.21.0 plus communicator, conversation, forex and user-management clients." },
    { id: "api-node-fsbo", name: "api-node-fsbo", kind: "api", repo: "boatsgroup/api-node-fsbo", host: "github", version: "3.0.3", lang: "js", tier: "tier-2", owner: "marketplace", system: "Marketplace", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-fsbo:3.0.3", port: 3081, deps: ["api-node-boats", "lib-common", "lib-config", "lib-logging", "dmm-clients"], stores: ["MySQL"], crit: 2, high: 4, med: 8, low: 9, incidents: 1, workItems: 6, freshness: "lagging", truth: "derived", coverage: 0.71, calls: 21, callers: 1, blastRadius: 4, story: "api-node-fsbo (for-sale-by-owner) is the oldest service in the set — plain JavaScript on Hapi 20, MySQL, and Salesforce sync via jsforce. It carries the heaviest legacy-dependency surface (request@2.88.2, aws-sdk v2, swig) and pins api-node-boats-client ^3.26.0." },
    { id: "api-node-forex", name: "api-node-forex", kind: "api", repo: "boatsgroup/api-node-forex", host: "github", version: "3.1.0", lang: "ts", tier: "tier-2", owner: "platform", system: "FX", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-forex:3.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common", "lib-logging"], stores: ["ElastiCache (Memcached)"], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 9, callers: 4, blastRadius: 9, story: "api-node-forex serves currency conversion rates used across boats, platform and boattrader. Small, stable, and heavily depended on for price normalization." },
    { id: "api-node-external-search", name: "api-node-external-search", kind: "api", repo: "boatsgroup/api-node-external-search", host: "github", version: "2.4.0", lang: "ts", tier: "tier-2", owner: "core-engineering", system: "Boat-Search", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-external-search:2.4.0", port: 3081, deps: ["api-node-boats", "lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.88, calls: 16, callers: 2, blastRadius: 5, story: "api-node-external-search exposes the public-facing search surface over Elasticsearch, fronting api-node-boats for partner and syndication traffic." },
    { id: "api-node-saved-search", name: "api-node-saved-search", kind: "api", repo: "boatsgroup/api-node-saved-search", host: "github", version: "1.8.2", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Boat-Search", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-saved-search:1.8.2", port: 3081, deps: ["api-node-boats", "api-node-external-search", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "stale", truth: "inferred", coverage: 0.64, calls: 11, callers: 1, blastRadius: 3, story: "api-node-saved-search stores user saved searches and alert criteria. Its runtime topology is partially inferred — deployment evidence in bg-prod is stale." },
    { id: "api-node-make-model", name: "api-node-make-model", kind: "api", repo: "boatsgroup/api-node-make-model", host: "github", version: "2.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Boat-Search", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-make-model:2.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 0, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.86, calls: 7, callers: 3, blastRadius: 4, story: "api-node-make-model serves the boat make/model taxonomy used to normalize listings and power faceted search." },
    { id: "api-node-datax", name: "api-node-datax", kind: "api", repo: "boatsgroup/api-node-datax", host: "github", version: "1.5.1", lang: "ts", tier: "tier-3", owner: "data", system: "Data", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-datax:1.5.1", port: 3081, deps: ["lib-common", "lib-logging"], stores: ["S3"], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.82, calls: 5, callers: 1, blastRadius: 2, story: "api-node-datax exposes data-export feeds to S3 for downstream analytics and partner syndication." },
    { id: "api-node-myboats", name: "api-node-myboats", kind: "api", repo: "boatsgroup/api-node-myboats", host: "github", version: "1.2.0", lang: "ts", tier: "tier-3", owner: "platform", system: "Platform", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-myboats:1.2.0", port: 3081, deps: ["api-node-boats", "api-node-platform", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.84, calls: 9, callers: 1, blastRadius: 2, story: "api-node-myboats manages a buyer's saved boats and comparison sets, reading through platform and boats." },
    { id: "api-node-salesforce-sync", name: "api-node-salesforce-sync", kind: "api", repo: "boatsgroup/api-node-salesforce-sync", host: "github", version: "2.0.4", lang: "ts", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/api-node-salesforce-sync:2.0.4", port: 3081, deps: ["api-node-fsbo", "lib-common"], stores: ["Salesforce"], crit: 0, high: 2, med: 3, low: 5, incidents: 0, workItems: 2, freshness: "fresh", truth: "derived", coverage: 0.74, calls: 8, callers: 0, blastRadius: 1, story: "api-node-salesforce-sync reconciles FSBO lead and account data into Salesforce. Outbound-only — no internal callers indexed." },
    // shared libraries (high blast radius, no runtime)
    { id: "lib-api-hapi", name: "lib-api-hapi", kind: "lib", repo: "boatsgroup/lib-api-hapi", host: "github", version: "17.7.2", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 1, high: 2, med: 4, low: 6, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.97, calls: 0, callers: 11, blastRadius: 26, story: "@dmm/lib-api-hapi is the shared Hapi server scaffold (auth, validation, routing, health). Imported by every api-node-* service — the highest-blast-radius package in the org." },
    { id: "lib-common", name: "lib-common", kind: "lib", repo: "boatsgroup/lib-common", host: "github", version: "7.12.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.98, calls: 0, callers: 13, blastRadius: 24, story: "@dmm/lib-common holds shared types, utilities and config helpers used across the entire Node estate." },
    { id: "lib-logging", name: "lib-logging", kind: "lib", repo: "boatsgroup/lib-logging", host: "github", version: "7.1.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 0, med: 2, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.99, calls: 0, callers: 12, blastRadius: 22, story: "@dmm/lib-logging is the structured logging library (pino-based) standard across all services." },
    { id: "lib-node-caching", name: "lib-node-caching", kind: "lib", repo: "boatsgroup/lib-node-caching", host: "github", version: "1.4.4", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 0, callers: 4, blastRadius: 8, story: "@dmm/lib-node-caching wraps Memcached/Valkey access with a consistent cache-aside interface." },
    { id: "lib-config", name: "lib-config", kind: "lib", repo: "boatsgroup/lib-config", host: "github", version: "5.0.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 0, med: 1, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 0, callers: 3, blastRadius: 5, story: "@dmm/lib-config resolves layered configuration (file + SSM + env) for services and jobs." },
    { id: "dmm-clients", name: "dmm-clients", kind: "lib", repo: "boatsgroup/dmm-clients", host: "github", version: "19.0.0", lang: "ts", tier: "lib", owner: "platform", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 0, callers: 9, blastRadius: 16, story: "@dmm/dmm-clients is the aggregate HTTP-client bundle. Consumers pin a wide range of majors (^17 → ^19), a notable version-skew surface." },
    // web + jobs
    { id: "boatsdotcom", name: "boatsdotcom", kind: "web", repo: "boatsgroup/boatsdotcom", host: "github", version: "—", lang: "ts", tier: "tier-1", owner: "storefront", system: "Storefront", envs: ["bg-prod", "bg-qa"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/boatsdotcom:portal", port: 8080, deps: ["api-node-platform", "api-node-boats"], stores: [], crit: 1, high: 3, med: 6, low: 14, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.85, calls: 0, callers: 0, blastRadius: 3, story: "boatsdotcom is the customer-facing Boats.com portal (Node SSR). It calls the platform orchestrator and search, fronted by API Gateway + CloudFront." },
    { id: "webapp-node-fsbo", name: "webapp-node-fsbo", kind: "web", repo: "boatsgroup/webapp-node-fsbo", host: "github", version: "—", lang: "js", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/webapp-node-fsbo:portal", port: 8080, deps: ["api-node-fsbo"], stores: [], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "lagging", truth: "derived", coverage: 0.68, calls: 0, callers: 0, blastRadius: 2, story: "webapp-node-fsbo is the for-sale-by-owner listing flow front-end, served from the fsbo API." },
    { id: "job-node-sitemaps-generator", name: "job-node-sitemaps-generator", kind: "job", repo: "boatsgroup/job-node-sitemaps-generator", host: "github", version: "1.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Boat-Search", envs: ["bg-prod"], image: "bg.dkr.ecr.us-east-1.amazonaws.com/job-node-sitemaps-generator:1.1.0", port: null, deps: ["api-node-boats", "api-node-external-search"], stores: ["S3"], crit: 0, high: 0, med: 1, low: 2, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.8, calls: 4, callers: 0, blastRadius: 1, story: "job-node-sitemaps-generator is a scheduled CronJob that crawls boats + external-search to build sitemaps and writes them to S3." }
  ];

  // -------------------------------------------------- vulnerabilities (tied to real packages)
  const vulns = [
    { cve: "CVE-2023-28155", pkg: "request", version: "2.88.2", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.34, kev: false, fixAvailable: false, fixed: null, services: ["api-node-fsbo"], firstSeen: "from manifest", title: "SSRF via cross-protocol redirect in deprecated request", source: "GHSA", prov: "derived" },
    { cve: "GHSA-aws-sdk-v2-eol", pkg: "aws-sdk", version: "2.1472.0", ecosystem: "npm", severity: "high", cvss: 7.0, epss: 0.12, kev: false, fixAvailable: true, fixed: "@aws-sdk/* v3", services: ["api-node-fsbo"], firstSeen: "from manifest", title: "aws-sdk v2 is end-of-life — no security maintenance", source: "npm Registry", prov: "derived" },
    { cve: "GHSA-swig-unmaintained", pkg: "swig", version: "1.4.2", ecosystem: "npm", severity: "high", cvss: 6.8, epss: 0.09, kev: false, fixAvailable: false, fixed: null, services: ["api-node-fsbo"], firstSeen: "from manifest", title: "Unmaintained template engine — XSS / RCE surface", source: "OSV", prov: "derived" },
    { cve: "CVE-2024-21538", pkg: "cross-spawn", version: "7.0.3", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.28, kev: false, fixAvailable: true, fixed: "7.0.5", services: ["api-node-platform", "api-node-boattrader"], firstSeen: "3d ago", title: "ReDoS in cross-spawn argument parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-4067", pkg: "micromatch", version: "4.0.5", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "4.0.8", services: ["api-node-boats", "api-node-forex"], firstSeen: "5d ago", title: "ReDoS in micromatch braces", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21536", pkg: "http-proxy-middleware", version: "2.0.6", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.18, kev: false, fixAvailable: true, fixed: "2.0.7", services: ["boatsdotcom"], firstSeen: "2d ago", title: "DoS via unhandled promise rejection", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-37890", pkg: "ws", version: "7.5.10", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.22, kev: false, fixAvailable: true, fixed: "8.17.1", services: ["api-node-conversation"], firstSeen: "4d ago", title: "DoS via excessive HTTP headers in ws", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21626", pkg: "runc (node-api-base)", version: "1.1.7", ecosystem: "deb", severity: "critical", cvss: 8.6, epss: 0.71, kev: false, fixAvailable: true, fixed: "1.1.12", services: ["api-node-boats", "api-node-platform", "api-node-boattrader", "api-node-fsbo"], firstSeen: "1d ago", title: "Container escape in base image OCI runtime", source: "NVD", prov: "inferred" },
    { cve: "CVE-2023-45853", pkg: "zlib (alpine)", version: "1.2.13", ecosystem: "apk", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "1.3", services: ["api-node-boats", "api-node-forex", "api-node-make-model"], firstSeen: "9d ago", title: "Integer overflow in minizip (node:20-alpine)", source: "NVD", prov: "inferred" },
    { cve: "CVE-2022-25883", pkg: "semver", version: "7.5.4", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.05, kev: false, fixAvailable: true, fixed: "7.5.4", services: ["api-node-platform"], firstSeen: "11d ago", title: "ReDoS in semver range parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-29041", pkg: "express", version: "4.18.2", ecosystem: "npm", severity: "medium", cvss: 6.1, epss: 0.08, kev: false, fixAvailable: true, fixed: "4.19.2", services: ["boatsdotcom", "webapp-node-fsbo"], firstSeen: "7d ago", title: "Open redirect via malformed URLs", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-28849", pkg: "follow-redirects", version: "1.15.5", ecosystem: "npm", severity: "medium", cvss: 6.5, epss: 0.21, kev: false, fixAvailable: true, fixed: "1.15.6", services: ["api-node-boattrader", "api-node-platform"], firstSeen: "6d ago", title: "Credential leak on cross-origin redirect", source: "GHSA", prov: "inferred" }
  ];

  // -------------------------------------------------------------- findings
  const findings = [
    { id: "f1", type: "Version skew", severity: "high", entity: "api-node-boats", title: "api-node-boats-client pinned across 4 major-incompatible ranges", detail: "platform ^3.5.0, conversation ^3.21.0, boattrader ^3.21.0, fsbo ^3.26.0 — current published client is 3.24.0. platform is 19 minors behind.", truth: "exact", source: "package_registry", age: "live" },
    { id: "f2", type: "Vulnerability", severity: "critical", entity: "api-node-boats", title: "Base-image container-escape (CVE-2024-21626) on 4 prod services", detail: "node-api-base:1.0.0 ships an OCI runtime vulnerable to runc cwd escape; api-node-boats and 3 others deploy it to bg-prod.", truth: "inferred", source: "vulnerability_intelligence", age: "1d" },
    { id: "f3", type: "Source fragmentation", severity: "medium", entity: "api-node-boats", title: "Repos split across Bitbucket, GitHub Enterprise & github.com", detail: "api-node-boats & api-node-boattrader still on bitbucket.org/boats-group; api-node-platform on github.boats.com/DMM-NODE; the rest on github.com/boatsgroup. Mixed webhook + freshness paths.", truth: "exact", source: "git", age: "live" },
    { id: "f4", type: "Legacy dependency", severity: "high", entity: "api-node-fsbo", title: "Deprecated & EOL dependencies in production service", detail: "request@2.88.2 (deprecated), aws-sdk@2.1472.0 (EOL), swig@1.4.2 (unmaintained) all ship to bg-prod.", truth: "derived", source: "package_registry", age: "live" },
    { id: "f5", type: "Untracked repo", severity: "medium", entity: "api-node-boats-temp", title: "Scratch repo points at the same Bitbucket remote", detail: "api-node-boats-temp shares git@bitbucket.org:boats-group/api-node-boats.git with no catalog-info.yaml and no ArgoCD app — likely an abandoned working copy.", truth: "exact", source: "git", age: "live" },
    { id: "f6", type: "Missing evidence", severity: "medium", entity: "api-node-saved-search", title: "Stale deployment evidence in bg-prod", detail: "Last successful kubernetes observation for api-node-saved-search exceeded the freshness budget; runtime placement is inferred.", truth: "inferred", source: "kubernetes", age: "5h" },
    { id: "f7", type: "Missing evidence", severity: "low", entity: "api-node-boats", title: "No SBOM attestation for deployed image", detail: "api-node-boats:4.3.1 has no cosign SBOM referrer in ECR; package inventory derived from npm manifests only.", truth: "inferred", source: "sbom_attestation", age: "5h" },
    { id: "f8", type: "Incident", severity: "high", entity: "api-node-fsbo", title: "Active PagerDuty incident correlated to MySQL latency", detail: "P2 opened 26m ago; change events from the last fsbo deploy linked. PagerDuty collector is rate-limited so correlation is lagging.", truth: "inferred", source: "pagerduty", age: "26m" }
  ];

  // ---------------------------------------------------- relationship verbs
  const relationships = [
    { verb: "IMPORTS", layer: "code", count: 2841, detail: "@dmm/* package import edges across the Node estate" },
    { verb: "CALLS", layer: "code", count: 1093, detail: "Symbol-level call edges within and across services" },
    { verb: "DEPLOYS_FROM", layer: "deploy", count: 41, detail: "ArgoCD app built & shipped from a source repo" },
    { verb: "DISCOVERS_CONFIG_IN", layer: "deploy", count: 82, detail: "ArgoCD/Kustomize overlay discovered in helm-charts" },
    { verb: "DECLARED_BY", layer: "infra", count: 1184, detail: "AWS resource declared by Terraform (IRSA, SG, Route53…)" },
    { verb: "RUNS_IN", layer: "runtime", count: 76, detail: "Workload placed in an EKS environment / namespace" },
    { verb: "DEPENDS_ON", layer: "runtime", count: 214, detail: "Runtime dependency between services" },
    { verb: "STORES_IN", layer: "infra", count: 38, detail: "Service reads/writes a datastore (ES, Memcached, MySQL…)" },
    { verb: "ASSUMES_ROLE", layer: "infra", count: 41, detail: "Workload assumes an IRSA role (Crossplane XIRSARole)" },
    { verb: "AFFECTED_BY", layer: "security", count: 162, detail: "Component affected by a known vulnerability" },
    { verb: "OBSERVED_INCIDENT", layer: "ops", count: 31, detail: "PagerDuty incident correlated to a service" },
    { verb: "TRACKED_BY", layer: "ops", count: 268, detail: "Jira work item linked to a service or change" }
  ];

  const layerColor = {
    code: "#14b8a6", deploy: "#ff8a00", infra: "#8b5cf6",
    runtime: "#4f8cff", security: "#f0506e", ops: "#22c55e"
  };

  const kindStyle = {
    service: { color: "#14b8a6", label: "Service" },
    repo: { color: "#f3ebdd", label: "Repository" },
    client: { color: "#2dd4bf", label: "npm Client" },
    library: { color: "#c4b59a", label: "Library" },
    image: { color: "#22d3ee", label: "Image" },
    workload: { color: "#4f8cff", label: "Workload" },
    env: { color: "#9ca3af", label: "Environment" },
    tf: { color: "#8b5cf6", label: "Terraform" },
    aws: { color: "#ff9d2e", label: "AWS Resource" },
    datastore: { color: "#f59e0b", label: "Datastore" },
    incident: { color: "#22c55e", label: "Incident" },
    vuln: { color: "#f0506e", label: "Vulnerability" },
    workitem: { color: "#60a5fa", label: "Work item" }
  };

  // ----------------------------------------- explorer graph (centered on api-node-boats)
  const graph = {
    nodes: [
      { id: "repo:boats", kind: "repo", label: "api-node-boats", sub: "bitbucket · boats-group", col: 0 },
      { id: "repo:forex", kind: "repo", label: "api-node-forex", sub: "github · boatsgroup", col: 0 },
      { id: "img:boats", kind: "image", label: "api-node-boats:4.3.1", sub: "ECR · node-api-base:1.0.0", col: 1 },
      { id: "client:boats", kind: "client", label: "@dmm/api-node-boats-client", sub: "npm · 3.24.0", col: 1 },
      { id: "svc:boats", kind: "service", label: "api-node-boats", sub: "tier-1 · Boat-Search", col: 2, hero: true },
      { id: "svc:forex", kind: "service", label: "api-node-forex", sub: "tier-2 · FX", col: 2 },
      { id: "svc:platform", kind: "service", label: "api-node-platform", sub: "consumer · ^3.5.0", col: 3 },
      { id: "svc:conversation", kind: "service", label: "api-node-conversation", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:boattrader", kind: "service", label: "api-node-boattrader", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:fsbo", kind: "service", label: "api-node-fsbo", sub: "consumer · ^3.26.0", col: 3 },
      { id: "lib:hapi", kind: "library", label: "@dmm/lib-api-hapi", sub: "17.7.2 · shared", col: 1 },
      { id: "wl:boats", kind: "workload", label: "Deployment/api-node-boats", sub: "ns: api-node · :3081", col: 3 },
      { id: "env:bgprod", kind: "env", label: "bg-prod", sub: "EKS · us-east-1", col: 4 },
      { id: "env:bgqa", kind: "env", label: "bg-qa", sub: "EKS · us-east-1", col: 4 },
      { id: "ds:es", kind: "datastore", label: "Elasticsearch", sub: "boat-search index", col: 2 },
      { id: "ds:cache", kind: "datastore", label: "ElastiCache", sub: "Memcached", col: 2 },
      { id: "tf:irsa", kind: "tf", label: "XIRSARole/api-node-boats", sub: "Crossplane · IRSA", col: 3 },
      { id: "aws:role", kind: "aws", label: "IAM Role", sub: "irsa · es + secrets read", col: 4 },
      { id: "vuln:base", kind: "vuln", label: "CVE-2024-21626", sub: "base image · CVSS 8.6", col: 2 },
      { id: "wi:boats", kind: "workitem", label: "DMM-3471", sub: "Jira · in progress", col: 1 }
    ],
    edges: [
      { s: "repo:boats", t: "img:boats", verb: "BUILDS", layer: "deploy" },
      { s: "repo:boats", t: "client:boats", verb: "PUBLISHES", layer: "deploy" },
      { s: "img:boats", t: "svc:boats", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "repo:forex", t: "svc:forex", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "svc:boats", t: "svc:forex", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:boats", t: "lib:hapi", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "client:boats", verb: "IMPORTS", layer: "code" },
      { s: "svc:conversation", t: "client:boats", verb: "IMPORTS", layer: "code" },
      { s: "svc:boattrader", t: "client:boats", verb: "IMPORTS", layer: "code" },
      { s: "svc:fsbo", t: "client:boats", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "svc:boats", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:boattrader", t: "svc:boats", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:boats", t: "wl:boats", verb: "RUNS_AS", layer: "runtime" },
      { s: "wl:boats", t: "env:bgprod", verb: "RUNS_IN", layer: "runtime" },
      { s: "wl:boats", t: "env:bgqa", verb: "RUNS_IN", layer: "runtime" },
      { s: "svc:boats", t: "ds:es", verb: "STORES_IN", layer: "infra" },
      { s: "svc:boats", t: "ds:cache", verb: "STORES_IN", layer: "infra" },
      { s: "wl:boats", t: "tf:irsa", verb: "ASSUMES_ROLE", layer: "infra" },
      { s: "tf:irsa", t: "aws:role", verb: "DECLARED_BY", layer: "infra" },
      { s: "img:boats", t: "vuln:base", verb: "AFFECTED_BY", layer: "security" },
      { s: "repo:boats", t: "wi:boats", verb: "TRACKED_BY", layer: "ops" }
    ]
  };

  const nodeDetail = {
    "svc:boats": { evidence: ["DEPLOYS_FROM api-node-boats:4.3.1 (ECR)", "RUNS_IN bg-prod, bg-qa · ns api-node :3081", "STORES_IN Elasticsearch, ElastiCache", "IMPORTS @dmm/lib-api-hapi, lib-common, lib-logging", "owner core-engineering · system Boat-Search"], freshness: "fresh", truth: "exact" },
    "client:boats": { evidence: ["PUBLISHED by api-node-boats (packages/client)", "current npm version 3.24.0", "consumers: platform ^3.5.0, conversation ^3.21.0, boattrader ^3.21.0, fsbo ^3.26.0"], freshness: "fresh", truth: "exact" },
    "vuln:base": { evidence: ["Base image node-api-base:1.0.0 (FROM Dockerfile)", "runc cwd container escape · CVSS 8.6", "Fix: rebuild base on patched runtime"], freshness: "fresh", truth: "inferred" },
    "tf:irsa": { evidence: ["Crossplane XIRSARole/api-node-boats", "patched per-overlay (bg-prod)", "grants Elasticsearch + SecretsManager read"], freshness: "fresh", truth: "exact" },
    "img:boats": { evidence: ["FROM node:20-alpine → node-api-base:1.0.0", "PM2 runtime · EXPOSE 8080 → svc :3081", "built via npm ci --ignore-scripts"], freshness: "fresh", truth: "exact" }
  };

  const N = 48;
  const metrics = {
    ingestRate: series(11, N, 640, 200, 1),
    queueDepth: series(22, N, 210, 120, -2),
    deadLetters: series(33, N, 5, 5, -0.04),
    graphNodes: series(44, N, 246000, 900, 700),
    graphEdges: series(55, N, 884000, 2600, 2100),
    writeTps: series(66, N, 1900, 480, 1),
    queryP50: fseries(77, N, 3.8, 1.2, 0.0, 1),
    queryP95: fseries(88, N, 12, 5, 0.0, 1),
    queryP99: fseries(99, N, 31, 14, 0.0, 0),
    cacheHit: fseries(111, N, 97.1, 1.0, 0.0, 1),
    newVulns: series(123, 14, 3, 5, -0.05)
  };

  const runtime = {
    indexStatus: "complete",
    graphReady: true,
    repos: 34,
    services: services.length,
    workloads: 41,
    cloudResources: 1184,
    nodes: 247184,
    edges: 891402,
    queueOutstanding: 196,
    inFlight: 11,
    deadLetters: 3,
    succeeded: 142880,
    backend: "NornicDB",
    backendVersion: "v0.9.4",
    uptime: "11d 6h",
    profile: "local_full_stack"
  };

  const sev = { critical: "#f0506e", high: "#ff8a00", medium: "#f5b73d", low: "#14b8a6", info: "#6b7280" };
  const statusColor = { healthy: "#14b8a6", degraded: "#f5b73d", stale: "#f0506e" };
  const truthColor = { exact: "#14b8a6", derived: "#f5b73d", inferred: "#ff8a00", fallback: "#ff8a00" };
  const freshColor = { fresh: "#14b8a6", lagging: "#f5b73d", stale: "#f0506e", building: "#f5b73d", unavailable: "#f0506e" };

  // ---------------------------------------------- ArgoCD deployed-app registry
  // Real app names read from helm-charts/argocd/*. `indexed` = present in the
  // locally-cloned repo set (deep facts); others are deploy-only (graph sees the
  // ArgoCD Application + image, but source isn't in this workspace yet).
  const indexedIds = new Set(services.map((s) => s.id));
  const argocdServiceNames = [
    "api-node-ab-test-proxy", "api-node-ai-image-processor", "api-node-ai-product-description-generation",
    "api-node-ai-summary", "api-node-atvs", "api-node-boats", "api-node-boattrader", "api-node-brochure",
    "api-node-bw-home", "api-node-chat", "api-node-datastore", "api-node-datax", "api-node-editorial",
    "api-node-engines", "api-node-external-search", "api-node-forex", "api-node-geo", "api-node-html2pdf",
    "api-node-jwt", "api-node-leadsmart", "api-node-listing-monitor", "api-node-mail", "api-node-make-model",
    "api-node-mls-soldboats", "api-node-myboats", "api-node-platform", "api-node-poc-nlp-search",
    "api-node-provisioning", "api-node-salesforce-sync", "api-node-saved-search", "api-node-search",
    "api-node-user-management", "api-node-whisper", "api-node-yw-fsbo"
  ];
  const argocdPortalNames = [
    "boatsandoutboards", "boatsdotcom", "boatshop24", "boatshop24uk", "boattrader", "boot24",
    "botenbank", "botentekoop", "cosasdebarcos", "inautia", "topbarcos", "yachtfocus", "youboat"
  ];
  const argocdApps = argocdServiceNames.map((n) => ({ name: n, kind: "service", env: ["bg-prod", "bg-qa"], indexed: indexedIds.has(n) }))
    .concat(argocdPortalNames.map((n) => ({ name: n, kind: "portal", env: ["bg-prod"], indexed: indexedIds.has(n) })));

  const servicesById = {};
  services.forEach((s) => { servicesById[s.id] = s; });
  const apiIds = new Set(services.filter((s) => s.kind === "api").map((s) => s.id));

  // --------------------------------- full-estate graph (every indexed service + real edges)
  function buildEstateGraph() {
    function col(s) {
      if (s.id === "api-node-boats") return 2;
      if (s.kind === "web" || s.kind === "job") return 0;
      if (s.kind === "lib") return 4;
      return s.deps.some((d) => apiIds.has(d)) ? 1 : 3; // api consumer vs leaf
    }
    function gkind(s) { return s.kind === "lib" ? "library" : s.kind === "job" ? "workload" : "service"; }
    const nodes = services.map((s) => ({
      id: s.id, kind: gkind(s), label: s.name,
      sub: (s.tier === "lib" ? "lib" : s.tier) + " · " + s.system,
      col: col(s), hero: s.id === "api-node-boats", truth: s.truth
    }));
    const edges = [];
    services.forEach((s) => s.deps.forEach((d) => {
      if (!servicesById[d]) return;
      const isLib = servicesById[d].kind === "lib";
      edges.push({ s: s.id, t: d, verb: isLib ? "IMPORTS" : "DEPENDS_ON", layer: isLib ? "code" : "runtime" });
    }));
    return { nodes, edges };
  }

  // ------------------------------------------------- live Eshu HTTP API client
  // Matches the real apps/console contract: application/eshu.envelope+json,
  // Bearer auth, /eshu-api/ default base (Vite proxies -> 127.0.0.1:8080).
  // Envelope: { data, error:{code,message}, truth:{level,profile,freshness:{state},capability} }
  function EshuApiClient(opts) {
    opts = opts || {};
    const b = (opts.baseUrl || "/eshu-api/").trim();
    this.baseUrl = b.endsWith("/") ? b : b + "/";
    this.apiKey = (opts.apiKey || "").trim();
  }
  EshuApiClient.prototype._url = function (path) {
    const p = path.startsWith("/") ? path.slice(1) : path;
    const origin = (typeof location !== "undefined" && location.origin) || "http://localhost";
    const base = /^https?:\/\//.test(this.baseUrl) ? this.baseUrl : new URL(this.baseUrl, origin).toString();
    return new URL(p, base).toString();
  };
  EshuApiClient.prototype._headers = function (extra) {
    const h = Object.assign({ Accept: "application/eshu.envelope+json" }, extra || {});
    if (this.apiKey) h.Authorization = "Bearer " + this.apiKey;
    return h;
  };
  EshuApiClient.prototype.get = async function (path) {
    const res = await fetch(this._url(path), { headers: this._headers() });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const env = await res.json();
    if (env && env.error && env.error.code) throw new Error(env.error.code);
    return env; // { data, truth }
  };
  EshuApiClient.prototype.post = async function (path, body) {
    const res = await fetch(this._url(path), { method: "POST", headers: this._headers({ "Content-Type": "application/json" }), body: JSON.stringify(body || {}) });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const env = await res.json();
    if (env && env.error && env.error.code) throw new Error(env.error.code);
    return env;
  };

  // truth.level (exact|derived|fallback) -> console chip; freshness (fresh|stale|building|unavailable)
  function chipTruth(level) { return level === "fallback" ? "inferred" : (level || "exact"); }
  function chipFresh(state) { return state === "building" ? "lagging" : state === "unavailable" ? "stale" : (state || "fresh"); }

  // Hydrate the console view-models from the live API. Each section is independent:
  // a failing/absent endpoint falls back to demo and is marked in `prov`.
  async function loadLive(client) {
    const out = { prov: {}, truth: {} };
    async function section(key, fn) {
      try { const v = await fn(); if (v !== undefined && v !== null) { out[key] = v; out.prov[key] = "live"; } else { out.prov[key] = "empty"; } }
      catch (e) { out.prov[key] = "error:" + (e && e.message || "failed"); }
    }

    // ---- runtime (ecosystem overview + index status) ----
    await section("runtime", async () => {
      const rt = Object.assign({}, runtime);
      try {
        const eco = (await client.get("/api/v0/ecosystem/overview")).data || {};
        if (eco.repository_count != null) rt.repos = eco.repository_count;
        if (eco.workload_count != null) rt.workloads = eco.workload_count;
        if (eco.platform_count != null) rt.platforms = eco.platform_count;
        if (eco.instance_count != null) rt.instances = eco.instance_count;
      } catch (e) { /* overview optional */ }
      const env = await client.get("/api/v0/index-status");
      const st = env.data || {};
      const q = st.queue || {};
      rt.indexStatus = st.status || rt.indexStatus;
      if (st.repository_count != null) rt.repos = st.repository_count;
      rt.queueOutstanding = q.outstanding != null ? q.outstanding : (q.pending || 0);
      rt.inFlight = q.in_flight || 0;
      rt.deadLetters = q.dead_letter || 0;
      rt.succeeded = q.succeeded || 0;
      if (env.truth) { rt.profile = env.truth.profile || rt.profile; out.truth.runtime = env.truth; }
      rt._live = true;
      return rt;
    });

    // ---- catalog -> services ----
    await section("services", async () => {
      const env = await client.get("/api/v0/catalog?limit=2000&offset=0");
      const c = env.data || {};
      const list = [];
      (c.services || []).concat(c.workloads || []).forEach((w) => {
        list.push({
          id: w.id || w.name, name: w.name || w.id, kind: "api",
          repo: w.repo_name || w.repo_id || "", host: "", version: "—", lang: "ts",
          tier: "tier-2", owner: "—", system: w.kind || "Service",
          envs: w.environments || [], image: null, port: null, deps: [], stores: [],
          crit: 0, high: 0, med: 0, low: 0, incidents: 0, workItems: 0,
          freshness: chipFresh(w.materialization_status === "graph" ? "fresh" : "building"),
          truth: chipTruth((env.truth && env.truth.level) || "exact"),
          coverage: 1, calls: 0, callers: 0, blastRadius: 0,
          story: "Live catalog entry from the Eshu API" + (w.repo_name ? " · defined by " + w.repo_name : "") + "."
        });
      });
      (c.repositories || []).forEach((r) => {
        if (!list.find((s) => s.name === (r.name || r.id))) {
          list.push({ id: r.id || r.name, name: r.name || r.id, kind: "lib", repo: r.repo_slug || r.local_path || r.name, host: "", version: "—", lang: "ts", tier: "lib", owner: "—", system: "Repository", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 0, med: 0, low: 0, incidents: 0, workItems: 0, freshness: "fresh", truth: chipTruth((env.truth && env.truth.level) || "exact"), coverage: 1, calls: 0, callers: 0, blastRadius: 0, story: "Indexed repository from the live Eshu API." });
        }
      });
      return list.length ? list : null;
    });

    // ---- language inventory (chart) ----
    await section("langInventory", async () => {
      const env = await client.get("/api/v0/repositories/by-language?limit=100&offset=0");
      const d = env.data || {};
      const arr = d.languages || d.results || (Array.isArray(d) ? d : []);
      const rows = arr.map((x) => ({ label: x.language || x.name, value: x.count || x.repository_count || x.repositories || 0 })).filter((r) => r.label);
      return rows.length ? rows : null;
    });

    // ---- ingesters/collectors ----
    await section("collectors", async () => {
      const env = await client.get("/api/v0/status/ingesters");
      const d = env.data || {};
      const arr = d.ingesters || d.results || (Array.isArray(d) ? d : []);
      const rows = arr.map((g) => ({
        kind: (g.kind || g.ingester || "git").toLowerCase().replace(/[^a-z_]/g, "_"),
        instance: g.id || g.ingester || g.name || "ingester",
        status: (g.state || g.status || "healthy") === "healthy" ? "healthy" : (g.state === "degraded" ? "degraded" : "stale"),
        facts: g.fact_count || g.facts || 0, scopes: g.scope_count || g.scopes || 0,
        lastRun: g.last_run || g.updated_at || "—", latencyMs: g.latency_ms || 0,
        freshness: chipFresh(g.freshness || "fresh"), cadence: g.cadence || "—", note: g.note || g.detail || ""
      }));
      return rows.length ? rows : null;
    });

    // ---- findings (dead-code) ----
    await section("findings", async () => {
      const env = await client.post("/api/v0/code/dead-code", { limit: 25 });
      const d = env.data || {};
      const lvl = chipTruth((env.truth && env.truth.level) || "derived");
      const rows = (d.results || []).map((r, i) => ({
        id: "live-dc-" + i, type: "Dead code", severity: "low",
        entity: r.repo_name || r.repo_id || "repository", title: "Unreferenced symbol " + (r.name || "candidate"),
        detail: (r.file_path || "unknown") + (r.classification ? " · " + r.classification : ""),
        truth: lvl, source: "code", age: "live"
      }));
      return rows.length ? rows : null;
    });

    // ---- vulnerabilities (supply-chain impact findings) ----
    await section("vulns", async () => {
      const env = await client.get("/api/v0/supply-chain/impact/findings?limit=50");
      const d = env.data || {};
      const arr = d.findings || d.results || [];
      const sevMap = { critical: "critical", high: "high", medium: "medium", moderate: "medium", low: "low" };
      const rows = arr.map((v) => ({
        cve: v.advisory_id || v.cve || v.id || "ADVISORY", pkg: v.package || v.package_name || v.subject || "—",
        version: v.version || v.affected_version || "", ecosystem: v.ecosystem || "npm",
        severity: sevMap[(v.severity || "").toLowerCase()] || "medium",
        cvss: v.cvss || v.cvss_score || 0, epss: v.epss || 0, kev: !!(v.kev || v.known_exploited),
        fixAvailable: !!(v.fixed_version || v.fix_available), fixed: v.fixed_version || null,
        services: v.affected_services || v.services || (v.repository_id ? [v.repository_id] : []),
        firstSeen: "live", title: v.title || v.summary || v.advisory_id || "Advisory",
        source: v.source || "supply-chain", prov: chipTruth((env.truth && env.truth.level) || "exact") === "inferred" ? "inferred" : "derived"
      }));
      return rows.length ? rows : null;
    });

    return out;
  }

  window.ESHU = {
    ENV, lang, collectorKinds, collectors, services, vulns, findings,
    relationships, layerColor, kindStyle, graph, nodeDetail, metrics, runtime,
    sev, statusColor, truthColor, freshColor,
    org: "demo",
    argocdApps, servicesById, buildEstateGraph, EshuApiClient, loadLive,
    util: { mulberry32, series, fseries }
  };
})();
