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

  const ENV = ["prod", "qa", "ops-test"];

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

  // collectors framed against the real sampleorg stack
  const collectors = [
    { kind: "git", instance: "git-multi-host", status: "degraded", facts: 142880, scopes: 34, lastRun: "1m ago", latencyMs: 410, freshness: "lagging", cadence: "webhook + 10m poll", note: "34 repos across Bitbucket, GitHub Enterprise & github.com" },
    { kind: "kubernetes", instance: "eks-observer", status: "healthy", facts: 61240, scopes: 3, lastRun: "40s ago", latencyMs: 360, freshness: "fresh", cadence: "watch", note: "prod, qa, ops-test · ArgoCD-synced workloads" },
    { kind: "aws", instance: "aws-bg", status: "healthy", facts: 88410, scopes: 2, lastRun: "3m ago", latencyMs: 1720, freshness: "fresh", cadence: "30m claim", note: "IRSA, SecretsManager, Route53, API Gateway, ElastiCache, ACM" },
    { kind: "terraform_state", instance: "tfstate-bg", status: "healthy", facts: 33120, scopes: 12, lastRun: "8m ago", latencyMs: 940, freshness: "fresh", cadence: "on-apply + 1h", note: "helm-charts/shared + terraform-stack-* + iac-eks-*" },
    { kind: "oci_registry", instance: "ecr-bg", status: "healthy", facts: 18760, scopes: 41, lastRun: "2m ago", latencyMs: 520, freshness: "fresh", cadence: "5m poll", note: "ECR · node-api-base:1.0.0 + 40 service images" },
    { kind: "package_registry", instance: "npm-dmm", status: "healthy", facts: 24910, scopes: 34, lastRun: "4m ago", latencyMs: 600, freshness: "fresh", cadence: "on-publish", note: "@sample/* internal scope · client + lib packages" },
    { kind: "vulnerability_intelligence", instance: "vuln-intel", status: "healthy", facts: 41203, scopes: 9, lastRun: "6m ago", latencyMs: 1310, freshness: "fresh", cadence: "15m claim", note: "CISA KEV · EPSS · NVD · OSV · GHSA" },
    { kind: "security_alert", instance: "dependabot", status: "healthy", facts: 6120, scopes: 34, lastRun: "5m ago", latencyMs: 430, freshness: "fresh", cadence: "10m poll", note: "GitHub Dependabot repository alerts" },
    { kind: "jira", instance: "jira-dmm", status: "healthy", facts: 12840, scopes: 6, lastRun: "7m ago", latencyMs: 720, freshness: "fresh", cadence: "webhook + 15m", note: "SAMPLE-NODE · work-item correlation" },
    { kind: "pagerduty", instance: "pd-prod", status: "degraded", facts: 2410, scopes: 8, lastRun: "21m ago", latencyMs: 4200, freshness: "lagging", cadence: "webhook + 15m", note: "Rate-limited by provider (429) — backing off" },
    { kind: "prometheus_mimir", instance: "mimir-bg", status: "healthy", facts: 21870, scopes: 3, lastRun: "30s ago", latencyMs: 230, freshness: "fresh", cadence: "1m poll", note: "Grafana dashboards shipped per ArgoCD app" },
    { kind: "sbom_attestation", instance: "sbom-attest", status: "stale", facts: 3110, scopes: 6, lastRun: "5h 12m ago", latencyMs: 0, freshness: "stale", cadence: "on-publish", note: "Few service images carry a cosign SBOM referrer" }
  ];

  // ------------------------------------------------------------- services
  // kind: api | lib | web | job ;  tier reflects criticality
  const services = [
    { id: "catalog-api", name: "catalog-api", kind: "api", repo: "sample-org/catalog-api", host: "bitbucket", version: "4.3.1", lang: "ts", tier: "tier-1", owner: "core-engineering", system: "Search", envs: ["prod", "qa"], image: "registry.example.internal/catalog-api:4.3.1", port: 3081, deps: ["rates-api", "lib-api-hapi", "lib-common", "lib-logging", "service-clients"], stores: ["Elasticsearch", "ElastiCache (Memcached)"], crit: 1, high: 3, med: 6, low: 11, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.96, calls: 34, callers: 12, blastRadius: 18, story: "catalog-api is the search API (Hapi/TypeScript, v4.3.1) owned by core-engineering in the Search system. It reads from Elasticsearch and caches in ElastiCache, builds on node:20-alpine → node-api-base:1.0.0, and deploys via ArgoCD/Kustomize to the api-node namespace on EKS (prod, qa). Its published @sample/catalog-api-client is consumed by platform, conversation, marketplace and fsbo." },
    { id: "platform-api", name: "platform-api", kind: "api", repo: "SAMPLE-NODE/platform-api", host: "ghe", version: "10.3.2", lang: "ts", tier: "tier-1", owner: "platform", system: "Platform", envs: ["prod", "qa"], image: "registry.example.internal/platform-api:10.3.2", port: 3081, deps: ["catalog-api", "messaging-api", "rates-api", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "service-clients"], stores: [], crit: 2, high: 5, med: 9, low: 17, incidents: 0, workItems: 8, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 61, callers: 6, blastRadius: 22, story: "platform-api (v10.3.2) is the orchestrator API. It composes a dozen internal clients — items, conversation, editorial, forex, spam-fraud, user-management, yw-fsbo, ai-provider — and is the widest internal consumer of catalog-api (pinned at ^3.5.0)." },
    { id: "messaging-api", name: "messaging-api", kind: "api", repo: "sampleorg/messaging-api", host: "github", version: "1.3.0", lang: "ts", tier: "tier-2", owner: "messaging", system: "Messaging", envs: ["prod", "qa"], image: "registry.example.internal/messaging-api:1.3.0", port: 3081, deps: ["catalog-api", "lib-api-hapi", "lib-logging"], stores: ["PostgreSQL (drizzle)"], crit: 0, high: 2, med: 5, low: 8, incidents: 1, workItems: 3, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 18, callers: 3, blastRadius: 7, story: "messaging-api manages SMS/WhatsApp threads via Twilio, persisting to PostgreSQL through drizzle-orm. It consumes catalog-api-client ^3.21.0 and is consumed in turn by platform and marketplace." },
    { id: "marketplace-api", name: "marketplace-api", kind: "api", repo: "sample-org/marketplace-api", host: "bitbucket", version: "1.3.0", lang: "ts", tier: "tier-1", owner: "marketplace", system: "Marketplace", envs: ["prod", "qa"], image: "registry.example.internal/marketplace-api:1.3.0", port: 3081, deps: ["catalog-api", "messaging-api", "rates-api", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "service-clients"], stores: ["Valkey"], crit: 1, high: 4, med: 7, low: 13, incidents: 0, workItems: 5, freshness: "fresh", truth: "exact", coverage: 0.91, calls: 44, callers: 2, blastRadius: 6, story: "marketplace-api powers the Marketplace Web web & mobile apps. It caches in Valkey, consumes catalog-api-client ^3.21.0 plus communicator, conversation, forex and user-management clients." },
    { id: "listings-api", name: "listings-api", kind: "api", repo: "sampleorg/listings-api", host: "github", version: "3.0.3", lang: "js", tier: "tier-2", owner: "marketplace", system: "Marketplace", envs: ["prod", "qa"], image: "registry.example.internal/listings-api:3.0.3", port: 3081, deps: ["catalog-api", "lib-common", "lib-config", "lib-logging", "service-clients"], stores: ["MySQL"], crit: 2, high: 4, med: 8, low: 9, incidents: 1, workItems: 6, freshness: "lagging", truth: "derived", coverage: 0.71, calls: 21, callers: 1, blastRadius: 4, story: "listings-api (for-sale-by-owner) is the oldest service in the set — plain JavaScript on Hapi 20, MySQL, and Salesforce sync via jsforce. It carries the heaviest legacy-dependency surface (request@2.88.2, aws-sdk v2, swig) and pins catalog-api-client ^3.26.0." },
    { id: "rates-api", name: "rates-api", kind: "api", repo: "sampleorg/rates-api", host: "github", version: "3.1.0", lang: "ts", tier: "tier-2", owner: "platform", system: "FX", envs: ["prod", "qa"], image: "registry.example.internal/rates-api:3.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common", "lib-logging"], stores: ["ElastiCache (Memcached)"], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 9, callers: 4, blastRadius: 9, story: "rates-api serves currency conversion rates used across items, platform and marketplace. Small, stable, and heavily depended on for price normalization." },
    { id: "external-search-api", name: "external-search-api", kind: "api", repo: "sampleorg/external-search-api", host: "github", version: "2.4.0", lang: "ts", tier: "tier-2", owner: "core-engineering", system: "Search", envs: ["prod", "qa"], image: "registry.example.internal/external-search-api:2.4.0", port: 3081, deps: ["catalog-api", "lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.88, calls: 16, callers: 2, blastRadius: 5, story: "external-search-api exposes the public-facing search surface over Elasticsearch, fronting catalog-api for partner and syndication traffic." },
    { id: "saved-search-api", name: "saved-search-api", kind: "api", repo: "sampleorg/saved-search-api", host: "github", version: "1.8.2", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Search", envs: ["prod"], image: "registry.example.internal/saved-search-api:1.8.2", port: 3081, deps: ["catalog-api", "external-search-api", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "stale", truth: "inferred", coverage: 0.64, calls: 11, callers: 1, blastRadius: 3, story: "saved-search-api stores user saved searches and alert criteria. Its runtime topology is partially inferred — deployment evidence in prod is stale." },
    { id: "taxonomy-api", name: "taxonomy-api", kind: "api", repo: "sampleorg/taxonomy-api", host: "github", version: "2.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Search", envs: ["prod"], image: "registry.example.internal/taxonomy-api:2.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 0, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.86, calls: 7, callers: 3, blastRadius: 4, story: "taxonomy-api serves the listing make/model taxonomy used to normalize listings and power faceted search." },
    { id: "data-export-api", name: "data-export-api", kind: "api", repo: "sampleorg/data-export-api", host: "github", version: "1.5.1", lang: "ts", tier: "tier-3", owner: "data", system: "Data", envs: ["prod"], image: "registry.example.internal/data-export-api:1.5.1", port: 3081, deps: ["lib-common", "lib-logging"], stores: ["S3"], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.82, calls: 5, callers: 1, blastRadius: 2, story: "data-export-api exposes data-export feeds to S3 for downstream analytics and partner syndication." },
    { id: "saved-items-api", name: "saved-items-api", kind: "api", repo: "sampleorg/saved-items-api", host: "github", version: "1.2.0", lang: "ts", tier: "tier-3", owner: "platform", system: "Platform", envs: ["prod"], image: "registry.example.internal/saved-items-api:1.2.0", port: 3081, deps: ["catalog-api", "platform-api", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.84, calls: 9, callers: 1, blastRadius: 2, story: "saved-items-api manages a buyer's saved items and comparison sets, reading through platform and items." },
    { id: "crm-sync-api", name: "crm-sync-api", kind: "api", repo: "sampleorg/crm-sync-api", host: "github", version: "2.0.4", lang: "ts", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["prod"], image: "registry.example.internal/crm-sync-api:2.0.4", port: 3081, deps: ["listings-api", "lib-common"], stores: ["Salesforce"], crit: 0, high: 2, med: 3, low: 5, incidents: 0, workItems: 2, freshness: "fresh", truth: "derived", coverage: 0.74, calls: 8, callers: 0, blastRadius: 1, story: "crm-sync-api reconciles FSBO lead and account data into Salesforce. Outbound-only — no internal callers indexed." },
    // shared libraries (high blast radius, no runtime)
    { id: "lib-api-hapi", name: "lib-api-hapi", kind: "lib", repo: "sampleorg/lib-api-hapi", host: "github", version: "17.7.2", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 1, high: 2, med: 4, low: 6, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.97, calls: 0, callers: 11, blastRadius: 26, story: "@sample/lib-api-hapi is the shared Hapi server scaffold (auth, validation, routing, health). Imported by every sample-* service — the highest-blast-radius package in the org." },
    { id: "lib-common", name: "lib-common", kind: "lib", repo: "sampleorg/lib-common", host: "github", version: "7.12.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.98, calls: 0, callers: 13, blastRadius: 24, story: "@sample/lib-common holds shared types, utilities and config helpers used across the entire Node estate." },
    { id: "lib-logging", name: "lib-logging", kind: "lib", repo: "sampleorg/lib-logging", host: "github", version: "7.1.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 0, med: 2, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.99, calls: 0, callers: 12, blastRadius: 22, story: "@sample/lib-logging is the structured logging library (pino-based) standard across all services." },
    { id: "lib-node-caching", name: "lib-node-caching", kind: "lib", repo: "sampleorg/lib-node-caching", host: "github", version: "1.4.4", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 0, callers: 4, blastRadius: 8, story: "@sample/lib-node-caching wraps Memcached/Valkey access with a consistent cache-aside interface." },
    { id: "lib-config", name: "lib-config", kind: "lib", repo: "sampleorg/lib-config", host: "github", version: "5.0.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 0, med: 1, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 0, callers: 3, blastRadius: 5, story: "@sample/lib-config resolves layered configuration (file + SSM + env) for services and jobs." },
    { id: "service-clients", name: "service-clients", kind: "lib", repo: "sampleorg/service-clients", host: "github", version: "19.0.0", lang: "ts", tier: "lib", owner: "platform", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 0, callers: 9, blastRadius: 16, story: "@sample/service-clients is the aggregate HTTP-client bundle. Consumers pin a wide range of majors (^17 → ^19), a notable version-skew surface." },
    // web + jobs
    { id: "marketplace-web", name: "marketplace-web", kind: "web", repo: "sampleorg/marketplace-web", host: "github", version: "—", lang: "ts", tier: "tier-1", owner: "storefront", system: "Storefront", envs: ["prod", "qa"], image: "registry.example.internal/marketplace-web:portal", port: 8080, deps: ["platform-api", "catalog-api"], stores: [], crit: 1, high: 3, med: 6, low: 14, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.85, calls: 0, callers: 0, blastRadius: 3, story: "marketplace-web is the customer-facing Example Marketplace portal (Node SSR). It calls the platform orchestrator and search, fronted by API Gateway + CloudFront." },
    { id: "webapp-node-fsbo", name: "webapp-node-fsbo", kind: "web", repo: "sampleorg/webapp-node-fsbo", host: "github", version: "—", lang: "js", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["prod"], image: "registry.example.internal/webapp-node-fsbo:portal", port: 8080, deps: ["listings-api"], stores: [], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "lagging", truth: "derived", coverage: 0.68, calls: 0, callers: 0, blastRadius: 2, story: "webapp-node-fsbo is the for-sale-by-owner listing flow front-end, served from the fsbo API." },
    { id: "job-node-sitemaps-generator", name: "job-node-sitemaps-generator", kind: "job", repo: "sampleorg/job-node-sitemaps-generator", host: "github", version: "1.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Search", envs: ["prod"], image: "registry.example.internal/job-node-sitemaps-generator:1.1.0", port: null, deps: ["catalog-api", "external-search-api"], stores: ["S3"], crit: 0, high: 0, med: 1, low: 2, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.8, calls: 4, callers: 0, blastRadius: 1, story: "job-node-sitemaps-generator is a scheduled CronJob that crawls items + external-search to build sitemaps and writes them to S3." }
  ];

  // -------------------------------------------------- vulnerabilities (tied to real packages)
  const vulns = [
    { cve: "CVE-2023-28155", pkg: "request", version: "2.88.2", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.34, kev: false, fixAvailable: false, fixed: null, services: ["listings-api"], firstSeen: "from manifest", title: "SSRF via cross-protocol redirect in deprecated request", source: "GHSA", prov: "derived" },
    { cve: "GHSA-aws-sdk-v2-eol", pkg: "aws-sdk", version: "2.1472.0", ecosystem: "npm", severity: "high", cvss: 7.0, epss: 0.12, kev: false, fixAvailable: true, fixed: "@aws-sdk/* v3", services: ["listings-api"], firstSeen: "from manifest", title: "aws-sdk v2 is end-of-life — no security maintenance", source: "npm Registry", prov: "derived" },
    { cve: "GHSA-swig-unmaintained", pkg: "swig", version: "1.4.2", ecosystem: "npm", severity: "high", cvss: 6.8, epss: 0.09, kev: false, fixAvailable: false, fixed: null, services: ["listings-api"], firstSeen: "from manifest", title: "Unmaintained template engine — XSS / RCE surface", source: "OSV", prov: "derived" },
    { cve: "CVE-2024-21538", pkg: "cross-spawn", version: "7.0.3", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.28, kev: false, fixAvailable: true, fixed: "7.0.5", services: ["platform-api", "marketplace-api"], firstSeen: "3d ago", title: "ReDoS in cross-spawn argument parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-4067", pkg: "micromatch", version: "4.0.5", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "4.0.8", services: ["catalog-api", "rates-api"], firstSeen: "5d ago", title: "ReDoS in micromatch braces", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21536", pkg: "http-proxy-middleware", version: "2.0.6", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.18, kev: false, fixAvailable: true, fixed: "2.0.7", services: ["marketplace-web"], firstSeen: "2d ago", title: "DoS via unhandled promise rejection", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-37890", pkg: "ws", version: "7.5.10", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.22, kev: false, fixAvailable: true, fixed: "8.17.1", services: ["messaging-api"], firstSeen: "4d ago", title: "DoS via excessive HTTP headers in ws", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21626", pkg: "runc (node-api-base)", version: "1.1.7", ecosystem: "deb", severity: "critical", cvss: 8.6, epss: 0.71, kev: false, fixAvailable: true, fixed: "1.1.12", services: ["catalog-api", "platform-api", "marketplace-api", "listings-api"], firstSeen: "1d ago", title: "Container escape in base image OCI runtime", source: "NVD", prov: "inferred" },
    { cve: "CVE-2023-45853", pkg: "zlib (alpine)", version: "1.2.13", ecosystem: "apk", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "1.3", services: ["catalog-api", "rates-api", "taxonomy-api"], firstSeen: "9d ago", title: "Integer overflow in minizip (node:20-alpine)", source: "NVD", prov: "inferred" },
    { cve: "CVE-2022-25883", pkg: "semver", version: "7.5.4", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.05, kev: false, fixAvailable: true, fixed: "7.5.4", services: ["platform-api"], firstSeen: "11d ago", title: "ReDoS in semver range parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-29041", pkg: "express", version: "4.18.2", ecosystem: "npm", severity: "medium", cvss: 6.1, epss: 0.08, kev: false, fixAvailable: true, fixed: "4.19.2", services: ["marketplace-web", "webapp-node-fsbo"], firstSeen: "7d ago", title: "Open redirect via malformed URLs", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-28849", pkg: "follow-redirects", version: "1.15.5", ecosystem: "npm", severity: "medium", cvss: 6.5, epss: 0.21, kev: false, fixAvailable: true, fixed: "1.15.6", services: ["marketplace-api", "platform-api"], firstSeen: "6d ago", title: "Credential leak on cross-origin redirect", source: "GHSA", prov: "inferred" }
  ];

  // -------------------------------------------------------------- findings
  const findings = [
    { id: "f1", type: "Version skew", severity: "high", entity: "catalog-api", title: "catalog-api-client pinned across 4 major-incompatible ranges", detail: "platform ^3.5.0, conversation ^3.21.0, marketplace ^3.21.0, fsbo ^3.26.0 — current published client is 3.24.0. platform is 19 minors behind.", truth: "exact", source: "package_registry", age: "live" },
    { id: "f2", type: "Vulnerability", severity: "critical", entity: "catalog-api", title: "Base-image container-escape (CVE-2024-21626) on 4 prod services", detail: "node-api-base:1.0.0 ships an OCI runtime vulnerable to runc cwd escape; catalog-api and 3 others deploy it to prod.", truth: "inferred", source: "vulnerability_intelligence", age: "1d" },
    { id: "f3", type: "Source fragmentation", severity: "medium", entity: "catalog-api", title: "Repos split across Bitbucket, GitHub Enterprise & github.com", detail: "catalog-api & marketplace-api still on bitbucket.org/sample-org; platform-api on github.example-marketplace/SAMPLE-NODE; the rest on github.com/sampleorg. Mixed webhook + freshness paths.", truth: "exact", source: "git", age: "live" },
    { id: "f4", type: "Legacy dependency", severity: "high", entity: "listings-api", title: "Deprecated & EOL dependencies in production service", detail: "request@2.88.2 (deprecated), aws-sdk@2.1472.0 (EOL), swig@1.4.2 (unmaintained) all ship to prod.", truth: "derived", source: "package_registry", age: "live" },
    { id: "f5", type: "Untracked repo", severity: "medium", entity: "catalog-api-temp", title: "Scratch repo points at the same Bitbucket remote", detail: "catalog-api-temp shares git@bitbucket.org:sample-org/catalog-api.git with no catalog-info.yaml and no ArgoCD app — likely an abandoned working copy.", truth: "exact", source: "git", age: "live" },
    { id: "f6", type: "Missing evidence", severity: "medium", entity: "saved-search-api", title: "Stale deployment evidence in prod", detail: "Last successful kubernetes observation for saved-search-api exceeded the freshness budget; runtime placement is inferred.", truth: "inferred", source: "kubernetes", age: "5h" },
    { id: "f7", type: "Missing evidence", severity: "low", entity: "catalog-api", title: "No SBOM attestation for deployed image", detail: "catalog-api:4.3.1 has no cosign SBOM referrer in ECR; package inventory derived from npm manifests only.", truth: "inferred", source: "sbom_attestation", age: "5h" },
    { id: "f8", type: "Incident", severity: "high", entity: "listings-api", title: "Active PagerDuty incident correlated to MySQL latency", detail: "P2 opened 26m ago; change events from the last fsbo deploy linked. PagerDuty collector is rate-limited so correlation is lagging.", truth: "inferred", source: "pagerduty", age: "26m" }
  ];

  // ---------------------------------------------------- relationship verbs
  const relationships = [
    { verb: "IMPORTS", layer: "code", count: 2841, detail: "@sample/* package import edges across the Node estate" },
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

  // ----------------------------------------- explorer graph (centered on catalog-api)
  const graph = {
    nodes: [
      { id: "repo:items", kind: "repo", label: "catalog-api", sub: "bitbucket · sample-org", col: 0 },
      { id: "repo:forex", kind: "repo", label: "rates-api", sub: "github · sampleorg", col: 0 },
      { id: "img:items", kind: "image", label: "catalog-api:4.3.1", sub: "ECR · node-api-base:1.0.0", col: 1 },
      { id: "client:items", kind: "client", label: "@sample/catalog-api-client", sub: "npm · 3.24.0", col: 1 },
      { id: "svc:items", kind: "service", label: "catalog-api", sub: "tier-1 · Search", col: 2, hero: true },
      { id: "svc:forex", kind: "service", label: "rates-api", sub: "tier-2 · FX", col: 2 },
      { id: "svc:platform", kind: "service", label: "platform-api", sub: "consumer · ^3.5.0", col: 3 },
      { id: "svc:conversation", kind: "service", label: "messaging-api", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:marketplace", kind: "service", label: "marketplace-api", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:fsbo", kind: "service", label: "listings-api", sub: "consumer · ^3.26.0", col: 3 },
      { id: "lib:hapi", kind: "library", label: "@sample/lib-api-hapi", sub: "17.7.2 · shared", col: 1 },
      { id: "wl:items", kind: "workload", label: "Deployment/catalog-api", sub: "ns: api-node · :3081", col: 3 },
      { id: "env:bgprod", kind: "env", label: "prod", sub: "EKS · us-east-1", col: 4 },
      { id: "env:bgqa", kind: "env", label: "qa", sub: "EKS · us-east-1", col: 4 },
      { id: "ds:es", kind: "datastore", label: "Elasticsearch", sub: "search index", col: 2 },
      { id: "ds:cache", kind: "datastore", label: "ElastiCache", sub: "Memcached", col: 2 },
      { id: "tf:irsa", kind: "tf", label: "XIRSARole/catalog-api", sub: "Crossplane · IRSA", col: 3 },
      { id: "aws:role", kind: "aws", label: "IAM Role", sub: "irsa · es + secrets read", col: 4 },
      { id: "vuln:base", kind: "vuln", label: "CVE-2024-21626", sub: "base image · CVSS 8.6", col: 2 },
      { id: "wi:items", kind: "workitem", label: "TASK-3471", sub: "Jira · in progress", col: 1 }
    ],
    edges: [
      { s: "repo:items", t: "img:items", verb: "BUILDS", layer: "deploy" },
      { s: "repo:items", t: "client:items", verb: "PUBLISHES", layer: "deploy" },
      { s: "img:items", t: "svc:items", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "repo:forex", t: "svc:forex", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "svc:items", t: "svc:forex", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:items", t: "lib:hapi", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "client:items", verb: "IMPORTS", layer: "code" },
      { s: "svc:conversation", t: "client:items", verb: "IMPORTS", layer: "code" },
      { s: "svc:marketplace", t: "client:items", verb: "IMPORTS", layer: "code" },
      { s: "svc:fsbo", t: "client:items", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "svc:items", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:marketplace", t: "svc:items", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:items", t: "wl:items", verb: "RUNS_AS", layer: "runtime" },
      { s: "wl:items", t: "env:bgprod", verb: "RUNS_IN", layer: "runtime" },
      { s: "wl:items", t: "env:bgqa", verb: "RUNS_IN", layer: "runtime" },
      { s: "svc:items", t: "ds:es", verb: "STORES_IN", layer: "infra" },
      { s: "svc:items", t: "ds:cache", verb: "STORES_IN", layer: "infra" },
      { s: "wl:items", t: "tf:irsa", verb: "ASSUMES_ROLE", layer: "infra" },
      { s: "tf:irsa", t: "aws:role", verb: "DECLARED_BY", layer: "infra" },
      { s: "img:items", t: "vuln:base", verb: "AFFECTED_BY", layer: "security" },
      { s: "repo:items", t: "wi:items", verb: "TRACKED_BY", layer: "ops" }
    ]
  };

  const nodeDetail = {
    "svc:items": { evidence: ["DEPLOYS_FROM catalog-api:4.3.1 (ECR)", "RUNS_IN prod, qa · ns api-node :3081", "STORES_IN Elasticsearch, ElastiCache", "IMPORTS @sample/lib-api-hapi, lib-common, lib-logging", "owner core-engineering · system Search"], freshness: "fresh", truth: "exact" },
    "client:items": { evidence: ["PUBLISHED by catalog-api (packages/client)", "current npm version 3.24.0", "consumers: platform ^3.5.0, conversation ^3.21.0, marketplace ^3.21.0, fsbo ^3.26.0"], freshness: "fresh", truth: "exact" },
    "vuln:base": { evidence: ["Base image node-api-base:1.0.0 (FROM Dockerfile)", "runc cwd container escape · CVSS 8.6", "Fix: rebuild base on patched runtime"], freshness: "fresh", truth: "inferred" },
    "tf:irsa": { evidence: ["Crossplane XIRSARole/catalog-api", "patched per-overlay (prod)", "grants Elasticsearch + SecretsManager read"], freshness: "fresh", truth: "exact" },
    "img:items": { evidence: ["FROM node:20-alpine → node-api-base:1.0.0", "PM2 runtime · EXPOSE 8080 → svc :3081", "built via npm ci --ignore-scripts"], freshness: "fresh", truth: "exact" }
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
    "sample-ab-test-proxy", "sample-ai-image-processor", "sample-ai-product-description-generation",
    "sample-ai-summary", "sample-atvs", "catalog-api", "marketplace-api", "sample-brochure",
    "sample-bw-home", "sample-chat", "sample-datastore", "data-export-api", "sample-editorial",
    "sample-engines", "external-search-api", "rates-api", "sample-geo", "sample-html2pdf",
    "sample-jwt", "sample-leadsmart", "sample-listing-monitor", "sample-mail", "taxonomy-api",
    "sample-mls-solditems", "saved-items-api", "platform-api", "sample-poc-nlp-search",
    "sample-provisioning", "crm-sync-api", "saved-search-api", "sample-search",
    "sample-user-management", "sample-whisper", "sample-yw-fsbo"
  ];
  const argocdPortalNames = [
    "itemsandoutboards", "marketplace-web", "itemshop24", "itemshop24uk", "marketplace", "boot24",
    "botenbank", "botentekoop", "cosasdebarcos", "inautia", "topbarcos", "yachtfocus", "youlisting"
  ];
  const argocdApps = argocdServiceNames.map((n) => ({ name: n, kind: "service", env: ["prod", "qa"], indexed: indexedIds.has(n) }))
    .concat(argocdPortalNames.map((n) => ({ name: n, kind: "portal", env: ["prod"], indexed: indexedIds.has(n) })));

  const servicesById = {};
  services.forEach((s) => { servicesById[s.id] = s; });
  const apiIds = new Set(services.filter((s) => s.kind === "api").map((s) => s.id));

  // --------------------------------- full-estate graph (every indexed service + real edges)
  function buildEstateGraph() {
    function col(s) {
      if (s.id === "catalog-api") return 2;
      if (s.kind === "web" || s.kind === "job") return 0;
      if (s.kind === "lib") return 4;
      return s.deps.some((d) => apiIds.has(d)) ? 1 : 3; // api consumer vs leaf
    }
    function gkind(s) { return s.kind === "lib" ? "library" : s.kind === "job" ? "workload" : "service"; }
    const nodes = services.map((s) => ({
      id: s.id, kind: gkind(s), label: s.name,
      sub: (s.tier === "lib" ? "lib" : s.tier) + " · " + s.system,
      col: col(s), hero: s.id === "catalog-api", truth: s.truth
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
