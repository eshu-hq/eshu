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

  const ENV = ["acme-prod", "acme-qa", "ops-qa"];

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
    sbom_attestation: { label: "SBOM", color: "#2dd4bf", glyph: "sbom" },
    cloudwatch: { label: "CloudWatch", color: "#ff9d2e", glyph: "cw" },
    otel_traces: { label: "OpenTelemetry", color: "#7c93ff", glyph: "otel" },
    grafana_loki: { label: "Grafana Loki", color: "#f5b73d", glyph: "loki" },
    datadog: { label: "Datadog APM", color: "#8b5cf6", glyph: "dd" },
    cloudflare: { label: "Cloudflare Edge", color: "#ff8a00", glyph: "cf" },
    grafana_synthetic: { label: "Synthetics", color: "#22d3ee", glyph: "syn" }
  };

  // collectors framed against the real acme stack
  const collectors = [
    { kind: "git", instance: "git-multi-host", status: "degraded", facts: 142880, scopes: 34, lastRun: "1m ago", latencyMs: 410, freshness: "lagging", cadence: "webhook + 10m poll", note: "34 repos across Bitbucket, GitHub Enterprise & github.com" },
    { kind: "kubernetes", instance: "eks-observer", status: "healthy", facts: 61240, scopes: 3, lastRun: "40s ago", latencyMs: 360, freshness: "fresh", cadence: "watch", note: "acme-prod, acme-qa, ops-qa · ArgoCD-synced workloads" },
    { kind: "aws", instance: "aws-acme", status: "healthy", facts: 88410, scopes: 2, lastRun: "3m ago", latencyMs: 1720, freshness: "fresh", cadence: "30m claim", note: "IRSA, SecretsManager, Route53, API Gateway, ElastiCache, ACM" },
    { kind: "terraform_state", instance: "tfstate-acme", status: "healthy", facts: 33120, scopes: 12, lastRun: "8m ago", latencyMs: 940, freshness: "fresh", cadence: "on-apply + 1h", note: "helm-charts/shared + terraform-stack-* + iac-eks-*" },
    { kind: "oci_registry", instance: "ecr-acme", status: "healthy", facts: 18760, scopes: 41, lastRun: "2m ago", latencyMs: 520, freshness: "fresh", cadence: "5m poll", note: "ECR · node-api-base:1.0.0 + 40 service images" },
    { kind: "package_registry", instance: "npm-acme", status: "healthy", facts: 24910, scopes: 34, lastRun: "4m ago", latencyMs: 600, freshness: "fresh", cadence: "on-publish", note: "@acme/* internal scope · client + lib packages" },
    { kind: "vulnerability_intelligence", instance: "vuln-intel", status: "healthy", facts: 41203, scopes: 9, lastRun: "6m ago", latencyMs: 1310, freshness: "fresh", cadence: "15m claim", note: "CISA KEV · EPSS · NVD · OSV · GHSA" },
    { kind: "security_alert", instance: "dependabot", status: "healthy", facts: 6120, scopes: 34, lastRun: "5m ago", latencyMs: 430, freshness: "fresh", cadence: "10m poll", note: "GitHub Dependabot repository alerts" },
    { kind: "jira", instance: "jira-acme", status: "healthy", facts: 12840, scopes: 6, lastRun: "7m ago", latencyMs: 720, freshness: "fresh", cadence: "webhook + 15m", note: "ACME-NODE · work-item correlation" },
    { kind: "pagerduty", instance: "pd-prod", status: "degraded", facts: 2410, scopes: 8, lastRun: "21m ago", latencyMs: 4200, freshness: "lagging", cadence: "webhook + 15m", note: "Rate-limited by provider (429) — backing off" },
    { kind: "prometheus_mimir", instance: "mimir-acme", status: "healthy", facts: 21870, scopes: 3, lastRun: "30s ago", latencyMs: 230, freshness: "fresh", cadence: "1m poll", note: "Grafana dashboards shipped per ArgoCD app" },
    { kind: "cloudwatch", instance: "cw-acme", status: "healthy", facts: 47320, scopes: 2, lastRun: "1m ago", latencyMs: 880, freshness: "fresh", cadence: "5m poll", note: "Alarms, log groups & RDS/ElastiCache metrics across acme-prod, acme-qa" },
    { kind: "otel_traces", instance: "otel-collector", status: "healthy", facts: 58910, scopes: 3, lastRun: "20s ago", latencyMs: 140, freshness: "fresh", cadence: "stream", note: "Tail-sampled spans → service map · DEPENDS_ON edges from real traffic" },
    { kind: "grafana_loki", instance: "loki-acme", status: "degraded", facts: 19240, scopes: 3, lastRun: "2m ago", latencyMs: 2600, freshness: "lagging", cadence: "stream", note: "Structured logs; ingester backpressure on acme-prod shard 2" },
    { kind: "datadog", instance: "dd-apm", status: "healthy", facts: 26110, scopes: 4, lastRun: "45s ago", latencyMs: 410, freshness: "fresh", cadence: "1m claim", note: "APM service catalog + SLOs for portal & marketplace systems" },
    { kind: "cloudflare", instance: "cf-edge", status: "healthy", facts: 8420, scopes: 13, lastRun: "3m ago", latencyMs: 520, freshness: "fresh", cadence: "5m poll", note: "WAF events, edge routes & cache rules for 13 storefront portals" },
    { kind: "grafana_synthetic", instance: "synthetics-acme", status: "stale", facts: 1840, scopes: 8, lastRun: "3h 40m ago", latencyMs: 0, freshness: "stale", cadence: "5m probe", note: "Synthetic uptime probes; checks paused after Grafana migration" },
    { kind: "sbom_attestation", instance: "sbom-attest", status: "stale", facts: 3110, scopes: 6, lastRun: "5h 12m ago", latencyMs: 0, freshness: "stale", cadence: "on-publish", note: "Few service images carry a cosign SBOM referrer" }
  ];

  // ------------------------------------------------------------- services
  // kind: api | lib | web | job ;  tier reflects criticality
  const services = [
    { id: "svc-catalog", name: "svc-catalog", kind: "api", repo: "acme/svc-catalog", host: "bitbucket", version: "4.3.1", lang: "ts", tier: "tier-1", owner: "core-engineering", system: "Catalog-Search", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-catalog:4.3.1", port: 3081, deps: ["svc-forex", "lib-api-hapi", "lib-common", "lib-logging", "acme-clients"], stores: ["Elasticsearch", "ElastiCache (Memcached)"], crit: 1, high: 3, med: 6, low: 11, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.96, calls: 34, callers: 12, blastRadius: 18, story: "svc-catalog is the catalog-search API (Hapi/TypeScript, v4.3.1) owned by core-engineering in the Catalog-Search system. It reads from Elasticsearch and caches in ElastiCache, builds on node:20-alpine → node-api-base:1.0.0, and deploys via ArgoCD/Kustomize to the api-node namespace on EKS (acme-prod, acme-qa). Its published @acme/svc-catalog-client is consumed by platform, conversation, marketplace and classifieds." },
    { id: "svc-platform", name: "svc-platform", kind: "api", repo: "ACME-NODE/svc-platform", host: "ghe", version: "10.3.2", lang: "ts", tier: "tier-1", owner: "platform", system: "Platform", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-platform:10.3.2", port: 3081, deps: ["svc-catalog", "svc-conversation", "svc-forex", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "acme-clients"], stores: [], crit: 2, high: 5, med: 9, low: 17, incidents: 0, workItems: 8, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 61, callers: 6, blastRadius: 22, story: "svc-platform (v10.3.2) is the orchestrator API. It composes a dozen internal clients — catalog, conversation, editorial, forex, spam-fraud, user-management, yw-classifieds, ai-provider — and is the widest internal consumer of svc-catalog (pinned at ^3.5.0)." },
    { id: "svc-conversation", name: "svc-conversation", kind: "api", repo: "acme/svc-conversation", host: "github", version: "1.3.0", lang: "ts", tier: "tier-2", owner: "messaging", system: "Messaging", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-conversation:1.3.0", port: 3081, deps: ["svc-catalog", "lib-api-hapi", "lib-logging"], stores: ["PostgreSQL (drizzle)"], crit: 0, high: 2, med: 5, low: 8, incidents: 1, workItems: 3, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 18, callers: 3, blastRadius: 7, story: "svc-conversation manages SMS/WhatsApp threads via Twilio, persisting to PostgreSQL through drizzle-orm. It consumes svc-catalog-client ^3.21.0 and is consumed in turn by platform and marketplace." },
    { id: "svc-marketplace", name: "svc-marketplace", kind: "api", repo: "acme/svc-marketplace", host: "bitbucket", version: "1.3.0", lang: "ts", tier: "tier-1", owner: "marketplace", system: "Marketplace", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-marketplace:1.3.0", port: 3081, deps: ["svc-catalog", "svc-conversation", "svc-forex", "lib-api-hapi", "lib-common", "lib-logging", "lib-node-caching", "acme-clients"], stores: ["Valkey"], crit: 1, high: 4, med: 7, low: 13, incidents: 0, workItems: 5, freshness: "fresh", truth: "exact", coverage: 0.91, calls: 44, callers: 2, blastRadius: 6, story: "svc-marketplace powers the Marketplace web & mobile apps. It caches in Valkey, consumes svc-catalog-client ^3.21.0 plus communicator, conversation, forex and user-management clients." },
    { id: "svc-classifieds", name: "svc-classifieds", kind: "api", repo: "acme/svc-classifieds", host: "github", version: "3.0.3", lang: "js", tier: "tier-2", owner: "marketplace", system: "Marketplace", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-classifieds:3.0.3", port: 3081, deps: ["svc-catalog", "lib-common", "lib-config", "lib-logging", "acme-clients"], stores: ["MySQL"], crit: 2, high: 4, med: 8, low: 9, incidents: 1, workItems: 6, freshness: "lagging", truth: "derived", coverage: 0.71, calls: 21, callers: 1, blastRadius: 4, story: "svc-classifieds (for-sale-by-owner) is the oldest service in the set — plain JavaScript on Hapi 20, MySQL, and Salesforce sync via jsforce. It carries the heaviest legacy-dependency surface (request@2.88.2, aws-sdk v2, swig) and pins svc-catalog-client ^3.26.0." },
    { id: "svc-forex", name: "svc-forex", kind: "api", repo: "acme/svc-forex", host: "github", version: "3.1.0", lang: "ts", tier: "tier-2", owner: "platform", system: "FX", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-forex:3.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common", "lib-logging"], stores: ["ElastiCache (Memcached)"], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 9, callers: 4, blastRadius: 9, story: "svc-forex serves currency conversion rates used across catalog, platform and marketplace. Small, stable, and heavily depended on for price normalization." },
    { id: "svc-external-search", name: "svc-external-search", kind: "api", repo: "acme/svc-external-search", host: "github", version: "2.4.0", lang: "ts", tier: "tier-2", owner: "core-engineering", system: "Catalog-Search", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-external-search:2.4.0", port: 3081, deps: ["svc-catalog", "lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.88, calls: 16, callers: 2, blastRadius: 5, story: "svc-external-search exposes the public-facing search surface over Elasticsearch, fronting svc-catalog for partner and syndication traffic." },
    { id: "svc-saved-search", name: "svc-saved-search", kind: "api", repo: "acme/svc-saved-search", host: "github", version: "1.8.2", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Catalog-Search", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-saved-search:1.8.2", port: 3081, deps: ["svc-catalog", "svc-external-search", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "stale", truth: "inferred", coverage: 0.64, calls: 11, callers: 1, blastRadius: 3, story: "svc-saved-search stores user saved searches and alert criteria. Its runtime topology is partially inferred — deployment evidence in acme-prod is stale." },
    { id: "svc-taxonomy", name: "svc-taxonomy", kind: "api", repo: "acme/svc-taxonomy", host: "github", version: "2.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Catalog-Search", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-taxonomy:2.1.0", port: 3081, deps: ["lib-api-hapi", "lib-common"], stores: ["Elasticsearch"], crit: 0, high: 0, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.86, calls: 7, callers: 3, blastRadius: 4, story: "svc-taxonomy serves the product make/model taxonomy used to normalize listings and power faceted search." },
    { id: "svc-datax", name: "svc-datax", kind: "api", repo: "acme/svc-datax", host: "github", version: "1.5.1", lang: "ts", tier: "tier-3", owner: "data", system: "Data", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-datax:1.5.1", port: 3081, deps: ["lib-common", "lib-logging"], stores: ["S3"], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.82, calls: 5, callers: 1, blastRadius: 2, story: "svc-datax exposes data-export feeds to S3 for downstream analytics and partner syndication." },
    { id: "svc-favorites", name: "svc-favorites", kind: "api", repo: "acme/svc-favorites", host: "github", version: "1.2.0", lang: "ts", tier: "tier-3", owner: "platform", system: "Platform", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-favorites:1.2.0", port: 3081, deps: ["svc-catalog", "svc-platform", "lib-api-hapi"], stores: ["DynamoDB"], crit: 0, high: 1, med: 2, low: 4, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.84, calls: 9, callers: 1, blastRadius: 2, story: "svc-favorites manages a buyer's saved items and comparison sets, reading through platform and catalog." },
    { id: "svc-salesforce-sync", name: "svc-salesforce-sync", kind: "api", repo: "acme/svc-salesforce-sync", host: "github", version: "2.0.4", lang: "ts", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/svc-salesforce-sync:2.0.4", port: 3081, deps: ["svc-classifieds", "lib-common"], stores: ["Salesforce"], crit: 0, high: 2, med: 3, low: 5, incidents: 0, workItems: 2, freshness: "fresh", truth: "derived", coverage: 0.74, calls: 8, callers: 0, blastRadius: 1, story: "svc-salesforce-sync reconciles Classifieds lead and account data into Salesforce. Outbound-only — no internal callers indexed." },
    // shared libraries (high blast radius, no runtime)
    { id: "lib-api-hapi", name: "lib-api-hapi", kind: "lib", repo: "acme/lib-api-hapi", host: "github", version: "17.7.2", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 1, high: 2, med: 4, low: 6, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.97, calls: 0, callers: 11, blastRadius: 26, story: "@acme/lib-api-hapi is the shared Hapi server scaffold (auth, validation, routing, health). Imported by every svc-* service — the highest-blast-radius package in the org." },
    { id: "lib-common", name: "lib-common", kind: "lib", repo: "acme/lib-common", host: "github", version: "7.12.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 1, med: 3, low: 5, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.98, calls: 0, callers: 13, blastRadius: 24, story: "@acme/lib-common holds shared types, utilities and config helpers used across the entire Node estate." },
    { id: "lib-logging", name: "lib-logging", kind: "lib", repo: "acme/lib-logging", host: "github", version: "7.1.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 0, med: 2, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.99, calls: 0, callers: 12, blastRadius: 22, story: "@acme/lib-logging is the structured logging library (pino-based) standard across all services." },
    { id: "lib-node-caching", name: "lib-node-caching", kind: "lib", repo: "acme/lib-node-caching", host: "github", version: "1.4.4", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 1, med: 2, low: 3, incidents: 0, workItems: 1, freshness: "fresh", truth: "exact", coverage: 0.93, calls: 0, callers: 4, blastRadius: 8, story: "@acme/lib-node-caching wraps Memcached/Valkey access with a consistent cache-aside interface." },
    { id: "lib-config", name: "lib-config", kind: "lib", repo: "acme/lib-config", host: "github", version: "5.0.0", lang: "ts", tier: "lib", owner: "core-engineering", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common"], stores: [], crit: 0, high: 0, med: 1, low: 3, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.95, calls: 0, callers: 3, blastRadius: 5, story: "@acme/lib-config resolves layered configuration (file + SSM + env) for services and jobs." },
    { id: "acme-clients", name: "acme-clients", kind: "lib", repo: "acme/acme-clients", host: "github", version: "19.0.0", lang: "ts", tier: "lib", owner: "platform", system: "Shared Libraries", envs: [], image: null, port: null, deps: ["lib-common", "lib-logging"], stores: [], crit: 0, high: 2, med: 4, low: 7, incidents: 0, workItems: 2, freshness: "fresh", truth: "exact", coverage: 0.9, calls: 0, callers: 9, blastRadius: 16, story: "@acme/acme-clients is the aggregate HTTP-client bundle. Consumers pin a wide range of majors (^17 → ^19), a notable version-skew surface." },
    // web + jobs
    { id: "storefront-web", name: "storefront-web", kind: "web", repo: "acme/storefront-web", host: "github", version: "—", lang: "ts", tier: "tier-1", owner: "storefront", system: "Storefront", envs: ["acme-prod", "acme-qa"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/storefront-web:portal", port: 8080, deps: ["svc-platform", "svc-catalog"], stores: [], crit: 1, high: 3, med: 6, low: 14, incidents: 0, workItems: 4, freshness: "fresh", truth: "exact", coverage: 0.85, calls: 0, callers: 0, blastRadius: 3, story: "storefront-web is the customer-facing Acme Shop portal (Node SSR). It calls the platform orchestrator and search, fronted by API Gateway + CloudFront." },
    { id: "web-classifieds", name: "web-classifieds", kind: "web", repo: "acme/web-classifieds", host: "github", version: "—", lang: "js", tier: "tier-3", owner: "marketplace", system: "Marketplace", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/web-classifieds:portal", port: 8080, deps: ["svc-classifieds"], stores: [], crit: 0, high: 1, med: 3, low: 6, incidents: 0, workItems: 1, freshness: "lagging", truth: "derived", coverage: 0.68, calls: 0, callers: 0, blastRadius: 2, story: "web-classifieds is the for-sale-by-owner listing flow front-end, served from the classifieds API." },
    { id: "job-sitemaps-generator", name: "job-sitemaps-generator", kind: "job", repo: "acme/job-sitemaps-generator", host: "github", version: "1.1.0", lang: "ts", tier: "tier-3", owner: "core-engineering", system: "Catalog-Search", envs: ["acme-prod"], image: "acme.dkr.ecr.us-east-1.amazonaws.com/job-sitemaps-generator:1.1.0", port: null, deps: ["svc-catalog", "svc-external-search"], stores: ["S3"], crit: 0, high: 0, med: 1, low: 2, incidents: 0, workItems: 0, freshness: "fresh", truth: "exact", coverage: 0.8, calls: 4, callers: 0, blastRadius: 1, story: "job-sitemaps-generator is a scheduled CronJob that crawls catalog + external-search to build sitemaps and writes them to S3." }
  ];

  // -------------------------------------------------- vulnerabilities (tied to real packages)
  const vulns = [
    { cve: "CVE-2023-28155", pkg: "request", version: "2.88.2", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.34, kev: false, fixAvailable: false, fixed: null, services: ["svc-classifieds"], firstSeen: "from manifest", title: "SSRF via cross-protocol redirect in deprecated request", source: "GHSA", prov: "derived" },
    { cve: "GHSA-aws-sdk-v2-eol", pkg: "aws-sdk", version: "2.1472.0", ecosystem: "npm", severity: "high", cvss: 7.0, epss: 0.12, kev: false, fixAvailable: true, fixed: "@aws-sdk/* v3", services: ["svc-classifieds"], firstSeen: "from manifest", title: "aws-sdk v2 is end-of-life — no security maintenance", source: "npm Registry", prov: "derived" },
    { cve: "GHSA-swig-unmaintained", pkg: "swig", version: "1.4.2", ecosystem: "npm", severity: "high", cvss: 6.8, epss: 0.09, kev: false, fixAvailable: false, fixed: null, services: ["svc-classifieds"], firstSeen: "from manifest", title: "Unmaintained template engine — XSS / RCE surface", source: "OSV", prov: "derived" },
    { cve: "CVE-2024-21538", pkg: "cross-spawn", version: "7.0.3", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.28, kev: false, fixAvailable: true, fixed: "7.0.5", services: ["svc-platform", "svc-marketplace"], firstSeen: "3d ago", title: "ReDoS in cross-spawn argument parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-4067", pkg: "micromatch", version: "4.0.5", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "4.0.8", services: ["svc-catalog", "svc-forex"], firstSeen: "5d ago", title: "ReDoS in micromatch braces", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21536", pkg: "http-proxy-middleware", version: "2.0.6", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.18, kev: false, fixAvailable: true, fixed: "2.0.7", services: ["storefront-web"], firstSeen: "2d ago", title: "DoS via unhandled promise rejection", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-37890", pkg: "ws", version: "7.5.10", ecosystem: "npm", severity: "high", cvss: 7.5, epss: 0.22, kev: false, fixAvailable: true, fixed: "8.17.1", services: ["svc-conversation"], firstSeen: "4d ago", title: "DoS via excessive HTTP headers in ws", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-21626", pkg: "runc (node-api-base)", version: "1.1.7", ecosystem: "deb", severity: "critical", cvss: 8.6, epss: 0.71, kev: false, fixAvailable: true, fixed: "1.1.12", services: ["svc-catalog", "svc-platform", "svc-marketplace", "svc-classifieds"], firstSeen: "1d ago", title: "Container escape in base image OCI runtime", source: "NVD", prov: "inferred" },
    { cve: "CVE-2023-45853", pkg: "zlib (alpine)", version: "1.2.13", ecosystem: "apk", severity: "medium", cvss: 5.3, epss: 0.07, kev: false, fixAvailable: true, fixed: "1.3", services: ["svc-catalog", "svc-forex", "svc-taxonomy"], firstSeen: "9d ago", title: "Integer overflow in minizip (node:20-alpine)", source: "NVD", prov: "inferred" },
    { cve: "CVE-2022-25883", pkg: "semver", version: "7.5.4", ecosystem: "npm", severity: "medium", cvss: 5.3, epss: 0.05, kev: false, fixAvailable: true, fixed: "7.5.4", services: ["svc-platform"], firstSeen: "11d ago", title: "ReDoS in semver range parsing", source: "GHSA", prov: "inferred" },
    { cve: "CVE-2024-29041", pkg: "express", version: "4.18.2", ecosystem: "npm", severity: "medium", cvss: 6.1, epss: 0.08, kev: false, fixAvailable: true, fixed: "4.19.2", services: ["storefront-web", "web-classifieds"], firstSeen: "7d ago", title: "Open redirect via malformed URLs", source: "OSV", prov: "inferred" },
    { cve: "CVE-2024-28849", pkg: "follow-redirects", version: "1.15.5", ecosystem: "npm", severity: "medium", cvss: 6.5, epss: 0.21, kev: false, fixAvailable: true, fixed: "1.15.6", services: ["svc-marketplace", "svc-platform"], firstSeen: "6d ago", title: "Credential leak on cross-origin redirect", source: "GHSA", prov: "inferred" }
  ];

  // -------------------------------------------------------------- findings
  const findings = [
    { id: "f1", type: "Version skew", severity: "high", entity: "svc-catalog", title: "svc-catalog-client pinned across 4 major-incompatible ranges", detail: "platform ^3.5.0, conversation ^3.21.0, marketplace ^3.21.0, classifieds ^3.26.0 — current published client is 3.24.0. platform is 19 minors behind.", truth: "exact", source: "package_registry", age: "live" },
    { id: "f2", type: "Vulnerability", severity: "critical", entity: "svc-catalog", title: "Base-image container-escape (CVE-2024-21626) on 4 prod services", detail: "node-api-base:1.0.0 ships an OCI runtime vulnerable to runc cwd escape; svc-catalog and 3 others deploy it to acme-prod.", truth: "inferred", source: "vulnerability_intelligence", age: "1d" },
    { id: "f3", type: "Source fragmentation", severity: "medium", entity: "svc-catalog", title: "Repos split across Bitbucket, GitHub Enterprise & github.com", detail: "svc-catalog & svc-marketplace still on bitbucket.org/acme; svc-platform on ghe.example.com/ACME-NODE; the rest on github.com/acme. Mixed webhook + freshness paths.", truth: "exact", source: "git", age: "live" },
    { id: "f4", type: "Legacy dependency", severity: "high", entity: "svc-classifieds", title: "Deprecated & EOL dependencies in production service", detail: "request@2.88.2 (deprecated), aws-sdk@2.1472.0 (EOL), swig@1.4.2 (unmaintained) all ship to acme-prod.", truth: "derived", source: "package_registry", age: "live" },
    { id: "f5", type: "Untracked repo", severity: "medium", entity: "svc-catalog-temp", title: "Scratch repo points at the same Bitbucket remote", detail: "svc-catalog-temp shares git@bitbucket.org:acme/svc-catalog.git with no catalog-info.yaml and no ArgoCD app — likely an abandoned working copy.", truth: "exact", source: "git", age: "live" },
    { id: "f6", type: "Missing evidence", severity: "medium", entity: "svc-saved-search", title: "Stale deployment evidence in acme-prod", detail: "Last successful kubernetes observation for svc-saved-search exceeded the freshness budget; runtime placement is inferred.", truth: "inferred", source: "kubernetes", age: "5h" },
    { id: "f7", type: "Missing evidence", severity: "low", entity: "svc-catalog", title: "No SBOM attestation for deployed image", detail: "svc-catalog:4.3.1 has no cosign SBOM referrer in ECR; package inventory derived from npm manifests only.", truth: "inferred", source: "sbom_attestation", age: "5h" },
    { id: "f8", type: "Incident", severity: "high", entity: "svc-classifieds", title: "Active PagerDuty incident correlated to MySQL latency", detail: "P2 opened 26m ago; change events from the last classifieds deploy linked. PagerDuty collector is rate-limited so correlation is lagging.", truth: "inferred", source: "pagerduty", age: "26m" }
  ];

  // ---------------------------------------------------- relationship verbs
  const relationships = [
    { verb: "IMPORTS", layer: "code", count: 2841, detail: "@acme/* package import edges across the Node estate" },
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
    { verb: "TRACKED_BY", layer: "ops", count: 268, detail: "Jira work item linked to a service or change" },
    { verb: "EMITS_METRICS", layer: "ops", count: 312, detail: "Workload ships metrics to Prometheus/Mimir (Grafana)" },
    { verb: "TRACED_BY", layer: "ops", count: 188, detail: "Service emits OpenTelemetry spans to the collector" },
    { verb: "LOGS_TO", layer: "ops", count: 96, detail: "Workload streams structured logs to Grafana Loki" },
    { verb: "FRONTED_BY", layer: "infra", count: 47, detail: "Portal traffic fronted by a Cloudflare edge route + WAF" }
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
    workitem: { color: "#60a5fa", label: "Work item" },
    monitor: { color: "#ff8a00", label: "Telemetry" },
    edge: { color: "#f6821f", label: "Edge / CDN" }
  };

  // ----------------------------------------- explorer graph (centered on svc-catalog)
  const graph = {
    nodes: [
      { id: "repo:catalog", kind: "repo", label: "svc-catalog", sub: "bitbucket · acme", col: 0 },
      { id: "repo:forex", kind: "repo", label: "svc-forex", sub: "github · acme", col: 0 },
      { id: "img:catalog", kind: "image", label: "svc-catalog:4.3.1", sub: "ECR · node-api-base:1.0.0", col: 1 },
      { id: "client:catalog", kind: "client", label: "@acme/svc-catalog-client", sub: "npm · 3.24.0", col: 1 },
      { id: "svc:catalog", kind: "service", label: "svc-catalog", sub: "tier-1 · Catalog-Search", col: 2, hero: true },
      { id: "svc:forex", kind: "service", label: "svc-forex", sub: "tier-2 · FX", col: 2 },
      { id: "svc:platform", kind: "service", label: "svc-platform", sub: "consumer · ^3.5.0", col: 3 },
      { id: "svc:conversation", kind: "service", label: "svc-conversation", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:marketplace", kind: "service", label: "svc-marketplace", sub: "consumer · ^3.21.0", col: 3 },
      { id: "svc:classifieds", kind: "service", label: "svc-classifieds", sub: "consumer · ^3.26.0", col: 3 },
      { id: "lib:hapi", kind: "library", label: "@acme/lib-api-hapi", sub: "17.7.2 · shared", col: 1 },
      { id: "wl:catalog", kind: "workload", label: "Deployment/svc-catalog", sub: "ns: api-node · :3081", col: 3 },
      { id: "env:bgprod", kind: "env", label: "acme-prod", sub: "EKS · us-east-1", col: 4 },
      { id: "env:bgqa", kind: "env", label: "acme-qa", sub: "EKS · us-east-1", col: 4 },
      { id: "ds:es", kind: "datastore", label: "Elasticsearch", sub: "catalog-search index", col: 2 },
      { id: "ds:cache", kind: "datastore", label: "ElastiCache", sub: "Memcached", col: 2 },
      { id: "tf:irsa", kind: "tf", label: "XIRSARole/svc-catalog", sub: "Crossplane · IRSA", col: 3 },
      { id: "aws:role", kind: "aws", label: "IAM Role", sub: "irsa · es + secrets read", col: 4 },
      { id: "vuln:base", kind: "vuln", label: "CVE-2024-21626", sub: "base image · CVSS 8.6", col: 2 },
      { id: "wi:catalog", kind: "workitem", label: "OPS-3471", sub: "Jira · in progress", col: 1 },
      { id: "mon:metrics", kind: "monitor", label: "Prometheus / Mimir", sub: "metrics · Grafana", col: 4 },
      { id: "mon:traces", kind: "monitor", label: "OpenTelemetry", sub: "traces · service map", col: 1 },
      { id: "mon:logs", kind: "monitor", label: "Grafana Loki", sub: "logs · structured", col: 4 },
      { id: "mon:apm", kind: "monitor", label: "Datadog APM", sub: "traces · SLOs", col: 1 },
      { id: "svc:storefront-web", kind: "service", label: "storefront-web", sub: "portal · Storefront", col: 0 },
      { id: "edge:cf", kind: "edge", label: "Cloudflare Edge", sub: "WAF · edge routes", col: 0 }
    ],
    edges: [
      { s: "repo:catalog", t: "img:catalog", verb: "BUILDS", layer: "deploy" },
      { s: "repo:catalog", t: "client:catalog", verb: "PUBLISHES", layer: "deploy" },
      { s: "img:catalog", t: "svc:catalog", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "repo:forex", t: "svc:forex", verb: "DEPLOYS_FROM", layer: "deploy" },
      { s: "svc:catalog", t: "svc:forex", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:catalog", t: "lib:hapi", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "client:catalog", verb: "IMPORTS", layer: "code" },
      { s: "svc:conversation", t: "client:catalog", verb: "IMPORTS", layer: "code" },
      { s: "svc:marketplace", t: "client:catalog", verb: "IMPORTS", layer: "code" },
      { s: "svc:classifieds", t: "client:catalog", verb: "IMPORTS", layer: "code" },
      { s: "svc:platform", t: "svc:catalog", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:marketplace", t: "svc:catalog", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:catalog", t: "wl:catalog", verb: "RUNS_AS", layer: "runtime" },
      { s: "wl:catalog", t: "env:bgprod", verb: "RUNS_IN", layer: "runtime" },
      { s: "wl:catalog", t: "env:bgqa", verb: "RUNS_IN", layer: "runtime" },
      { s: "svc:catalog", t: "ds:es", verb: "STORES_IN", layer: "infra" },
      { s: "svc:catalog", t: "ds:cache", verb: "STORES_IN", layer: "infra" },
      { s: "wl:catalog", t: "tf:irsa", verb: "ASSUMES_ROLE", layer: "infra" },
      { s: "tf:irsa", t: "aws:role", verb: "DECLARED_BY", layer: "infra" },
      { s: "img:catalog", t: "vuln:base", verb: "AFFECTED_BY", layer: "security" },
      { s: "repo:catalog", t: "wi:catalog", verb: "TRACKED_BY", layer: "ops" },
      { s: "wl:catalog", t: "mon:metrics", verb: "EMITS_METRICS", layer: "ops" },
      { s: "svc:catalog", t: "mon:traces", verb: "TRACED_BY", layer: "ops" },
      { s: "wl:catalog", t: "mon:logs", verb: "LOGS_TO", layer: "ops" },
      { s: "svc:catalog", t: "mon:apm", verb: "TRACED_BY", layer: "ops" },
      { s: "svc:storefront-web", t: "svc:platform", verb: "DEPENDS_ON", layer: "runtime" },
      { s: "svc:storefront-web", t: "edge:cf", verb: "FRONTED_BY", layer: "infra" }
    ]
  };

  const nodeDetail = {
    "svc:catalog": { evidence: ["DEPLOYS_FROM svc-catalog:4.3.1 (ECR)", "RUNS_IN acme-prod, acme-qa · ns api-node :3081", "STORES_IN Elasticsearch, ElastiCache", "IMPORTS @acme/lib-api-hapi, lib-common, lib-logging", "owner core-engineering · system Catalog-Search"], freshness: "fresh", truth: "exact" },
    "client:catalog": { evidence: ["PUBLISHED by svc-catalog (packages/client)", "current npm version 3.24.0", "consumers: platform ^3.5.0, conversation ^3.21.0, marketplace ^3.21.0, classifieds ^3.26.0"], freshness: "fresh", truth: "exact" },
    "vuln:base": { evidence: ["Base image node-api-base:1.0.0 (FROM Dockerfile)", "runc cwd container escape · CVSS 8.6", "Fix: rebuild base on patched runtime"], freshness: "fresh", truth: "inferred" },
    "tf:irsa": { evidence: ["Crossplane XIRSARole/svc-catalog", "patched per-overlay (acme-prod)", "grants Elasticsearch + SecretsManager read"], freshness: "fresh", truth: "exact" },
    "img:catalog": { evidence: ["FROM node:20-alpine → node-api-base:1.0.0", "PM2 runtime · EXPOSE 8080 → svc :3081", "built via npm ci --ignore-scripts"], freshness: "fresh", truth: "exact" },
    "mon:metrics": { evidence: ["EMITS_METRICS to Prometheus / Grafana Mimir", "Grafana dashboard shipped per ArgoCD app", "RED + USE panels · p50/p95/p99 latency", "alert rules: error-rate, saturation"], freshness: "fresh", truth: "exact" },
    "mon:traces": { evidence: ["TRACED_BY OpenTelemetry collector (tail-sampled)", "spans derive DEPENDS_ON edges from real traffic", "exporter: OTLP/gRPC → service map", "trace-to-log correlation via Loki"], freshness: "fresh", truth: "derived" },
    "mon:logs": { evidence: ["LOGS_TO Grafana Loki (structured JSON)", "trace ⋈ log correlation by trace-id", "ingester backpressure on acme-prod shard 2", "collector: grafana_loki · streaming"], freshness: "lagging", truth: "derived" },
    "mon:apm": { evidence: ["TRACED_BY Datadog APM agent", "SLOs: availability 99.9% · latency p95 < 300ms", "service catalog joined to ArgoCD apps", "collector: datadog · 1m claim"], freshness: "fresh", truth: "derived" },
    "svc:storefront-web": { evidence: ["RUNS_IN acme-prod, acme-qa · Node SSR portal", "DEPENDS_ON svc-platform, svc-catalog", "FRONTED_BY Cloudflare edge (WAF + cache)", "owner storefront · system Storefront"], freshness: "fresh", truth: "exact" },
    "edge:cf": { evidence: ["FRONTED_BY Cloudflare for 13 storefront portals", "WAF events + cache rules + edge routes", "collector: cloudflare · 5m poll", "fronts storefront-web, marketplace, storefront-fr…"], freshness: "fresh", truth: "exact" }
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
    "svc-ab-test-proxy", "svc-ai-image-processor", "svc-ai-product-description-generation",
    "svc-ai-summary", "svc-atvs", "svc-catalog", "svc-marketplace", "svc-brochure",
    "svc-bw-home", "svc-chat", "svc-datastore", "svc-datax", "svc-editorial",
    "svc-engines", "svc-external-search", "svc-forex", "svc-geo", "svc-html2pdf",
    "svc-jwt", "svc-leadsmart", "svc-listing-monitor", "svc-mail", "svc-taxonomy",
    "svc-mls-sold", "svc-favorites", "svc-platform", "svc-poc-nlp-search",
    "svc-provisioning", "svc-salesforce-sync", "svc-saved-search", "svc-search",
    "svc-user-management", "svc-whisper", "svc-yw-classifieds"
  ];
  const argocdPortalNames = [
    "storefront-na", "storefront-web", "storefront-eu", "storefront-uk", "marketplace", "storefront-de",
    "storefront-nl", "storefront-be", "storefront-es", "storefront-pt", "storefront-br", "storefront-fr", "storefront-it"
  ];
  const argocdApps = argocdServiceNames.map((n) => ({ name: n, kind: "service", env: ["acme-prod", "acme-qa"], indexed: indexedIds.has(n) }))
    .concat(argocdPortalNames.map((n) => ({ name: n, kind: "portal", env: ["acme-prod"], indexed: indexedIds.has(n) })));

  const servicesById = {};
  services.forEach((s) => { servicesById[s.id] = s; });
  const apiIds = new Set(services.filter((s) => s.kind === "api").map((s) => s.id));

  // --------------------------------- full-estate graph (every indexed service + real edges)
  function buildEstateGraph() {
    function col(s) {
      if (s.id === "svc-catalog") return 2;
      if (s.kind === "web" || s.kind === "job") return 0;
      if (s.kind === "lib") return 4;
      return s.deps.some((d) => apiIds.has(d)) ? 1 : 3; // api consumer vs leaf
    }
    function gkind(s) { return s.kind === "lib" ? "library" : s.kind === "job" ? "workload" : "service"; }
    const nodes = services.map((s) => ({
      id: s.id, kind: gkind(s), label: s.name,
      sub: (s.tier === "lib" ? "lib" : s.tier) + " · " + s.system,
      col: col(s), hero: s.id === "svc-catalog", truth: s.truth
    }));
    const edges = [];
    services.forEach((s) => s.deps.forEach((d) => {
      if (!servicesById[d]) return;
      const isLib = servicesById[d].kind === "lib";
      edges.push({ s: s.id, t: d, verb: isLib ? "IMPORTS" : "DEPENDS_ON", layer: isLib ? "code" : "runtime" });
    }));
    return { nodes, edges };
  }

  // =============================================== cloud resources (CloudResource nodes)
  // Real model: canonical :CloudResource nodes keyed by uid = hash(account, region,
  // resource_type, resource_id). Multi-cloud (aws|azure|gcp). resource_type tokens are
  // the real scanner discriminators (constants_*.go). Observability objects (CloudWatch
  // alarms/log groups/dashboards, X-Ray, AMP, Grafana) are CloudResource nodes too.
  const cloudAccounts = [
    { id: "aws-prod", provider: "aws", label: "acme-prod", account: "111122223333", region: "us-east-1", env: "acme-prod" },
    { id: "aws-qa", provider: "aws", label: "acme-nonprod", account: "444455556666", region: "us-east-1", env: "acme-qa" },
    { id: "azure-edge", provider: "azure", label: "acme-edge-sub", account: "b7d1-edge", region: "eastus", env: "acme-prod" },
    { id: "gcp-data", provider: "gcp", label: "acme-data-warehouse", account: "acme-data-417", region: "us-central1", env: "acme-prod" }
  ];
  const resourceFamily = {
    aws_iam_role: "identity", aws_iam_policy: "identity", aws_iam_instance_profile: "identity", aws_eks_oidc_provider: "identity", aws_rolesanywhere_trust_anchor: "identity", aws_accessanalyzer_analyzer: "identity",
    aws_ec2_vpc: "networking", aws_vpc_endpoint: "networking", aws_vpc_nat_gateway: "networking", aws_security_group: "networking", aws_apigateway_rest_api: "networking", aws_globalaccelerator_accelerator: "networking",
    aws_eks_cluster: "compute", aws_eks_nodegroup: "compute", aws_ec2_instance: "compute", aws_autoscaling_group: "compute", aws_lambda_function: "compute",
    aws_s3_bucket: "storage", aws_dynamodb_table: "storage", aws_rds_db_instance: "storage", aws_elasticache_cluster: "storage", aws_opensearch_domain: "storage", aws_redshift_cluster: "storage",
    aws_sqs_queue: "messaging", aws_sns_topic: "messaging", aws_eventbridge_event_bus: "messaging",
    aws_cloudwatch_alarm: "observability", aws_cloudwatch_dashboard: "observability", aws_cloudwatch_logs_log_group: "observability", aws_xray_sampling_rule: "observability", aws_amp_workspace: "observability", aws_grafana_workspace: "observability", aws_synthetics_canary: "observability",
    azure_frontdoor_profile: "networking", azure_monitor_workspace: "observability",
    gcp_bigquery_dataset: "storage", gcp_cloud_run_service: "compute"
  };
  const cloudFamilies = {
    identity: { label: "Identity & IAM", color: "#ff9d2e", icon: "shield" },
    networking: { label: "Networking", color: "#4f8cff", icon: "branch" },
    compute: { label: "Compute", color: "#14b8a6", icon: "box" },
    storage: { label: "Storage & Data", color: "#f59e0b", icon: "db" },
    messaging: { label: "Messaging", color: "#8b5cf6", icon: "pulse" },
    observability: { label: "Observability", color: "#22c55e", icon: "spark" }
  };
  // signal taxonomy for the observability coverage read-model
  const signalKinds = {
    metrics: { label: "Metrics", color: "#ff8a00", sources: "Prometheus / CloudWatch", verb: "EMITS_METRICS" },
    logs: { label: "Logs", color: "#f5b73d", sources: "Loki / CloudWatch Logs", verb: "LOGS_TO" },
    traces: { label: "Traces", color: "#7c93ff", sources: "OpenTelemetry / X-Ray / Datadog", verb: "TRACED_BY" },
    dashboards: { label: "Dashboards", color: "#22d3ee", sources: "Grafana / CloudWatch", verb: "EMITS_METRICS" },
    alerts: { label: "Alerts / SLO", color: "#f0506e", sources: "CloudWatch Alarm / PagerDuty", verb: "OBSERVED_INCIDENT" },
    synthetics: { label: "Synthetics", color: "#2dd4bf", sources: "CloudWatch Synthetics", verb: "EMITS_METRICS" }
  };

  function crHash(account, region, type, rid) {
    let h = 2166136261; const s = account + "|" + region + "|" + type + "|" + rid;
    for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619); }
    return "cr-" + (h >>> 0).toString(16).padStart(8, "0");
  }
  function buildCloudResources() {
    const out = [];
    const rnd = mulberry32(7);
    const add = (provider, accId, type, rid, name, opts) => {
      const acc = cloudAccounts.find((a) => a.id === accId);
      out.push(Object.assign({
        uid: crHash(acc.account, acc.region, type, rid), provider, account: accId, region: acc.region,
        type, family: resourceFamily[type] || "compute", name, resourceId: rid,
        ref: provider === "aws" ? "arn:aws:…:" + acc.account + ":" + rid : provider === "azure" ? "/subscriptions/" + acc.account + "/…/" + rid : "//" + acc.account + "/" + rid,
        service: null, tf: true, truth: "exact", freshness: "fresh", signal: null
      }, opts || {}));
    };
    // shared platform infra (per env account)
    ["aws-prod", "aws-qa"].forEach((accId) => {
      const env = cloudAccounts.find((a) => a.id === accId).env;
      add("aws", accId, "aws_eks_cluster", "eks-" + env, "eks-" + env, { tf: true });
      add("aws", accId, "aws_eks_nodegroup", "eks-" + env + "-ng-api", "eks-" + env + "/ng-api-node", { name: "eks-" + env + "/ng-api-node" });
      add("aws", accId, "aws_eks_oidc_provider", "oidc-" + env, "oidc.eks." + env, {});
      add("aws", accId, "aws_ec2_vpc", "vpc-" + env, "vpc-" + env + " (10.40.0.0/16)", {});
      add("aws", accId, "aws_vpc_nat_gateway", "nat-" + env, "nat-" + env + "-az1", {});
      add("aws", accId, "aws_amp_workspace", "amp-" + env, "AMP / Prometheus (" + env + ")", { signal: "metrics" });
      add("aws", accId, "aws_grafana_workspace", "grafana-" + env, "Grafana workspace (" + env + ")", { signal: "dashboards" });
      add("aws", accId, "aws_accessanalyzer_analyzer", "aa-" + env, "access-analyzer-" + env, {});
    });
    // per-service resources, attached to the indexed services
    services.forEach((s, i) => {
      const accId = (s.envs && s.envs.includes("acme-qa") && i % 2) ? "aws-qa" : "aws-prod";
      const isRunning = s.kind !== "lib";
      if (isRunning) {
        add("aws", accId, "aws_iam_role", "irsa-" + s.name, "XIRSARole/" + s.name, { service: s.id, truth: "exact" });
        add("aws", accId, "aws_security_group", "sg-" + s.name, "sg-" + s.name + " (:3081)", { service: s.id });
        add("aws", accId, "aws_cloudwatch_logs_log_group", "/eks/" + s.name, "/aws/eks/" + s.name, { service: s.id, family: "observability", signal: "logs" });
        if (s.tier === "tier-1" || s.tier === "tier-2") {
          add("aws", accId, "aws_cloudwatch_alarm", "alarm-" + s.name + "-5xx", s.name + " · 5xx error-rate", { service: s.id, family: "observability", signal: "alerts", truth: s.truth });
        }
        if (s.tier === "tier-1") {
          add("aws", accId, "aws_xray_sampling_rule", "xray-" + s.name, s.name + " · trace sampling", { service: s.id, family: "observability", signal: "traces" });
          add("aws", accId, "aws_cloudwatch_dashboard", "dash-" + s.name, s.name + " · RED dashboard", { service: s.id, family: "observability", signal: "dashboards" });
        }
      }
      // datastores → CloudResource storage nodes
      (s.stores || []).forEach((st) => {
        const t = /Elasticsearch/.test(st) ? "aws_opensearch_domain" : /ElastiCache|Valkey|Memcached/.test(st) ? "aws_elasticache_cluster" : /DynamoDB/.test(st) ? "aws_dynamodb_table" : /S3/.test(st) ? "aws_s3_bucket" : /RDS|PostgreSQL|MySQL/.test(st) ? "aws_rds_db_instance" : null;
        if (t) add("aws", accId, t, t.replace("aws_", "") + "-" + s.name, st + " · " + s.name, { service: s.id, family: "storage" });
      });
    });
    // a couple of synthetics canaries (mostly absent → coverage gaps)
    add("aws", "aws-prod", "aws_synthetics_canary", "canary-storefront-web", "storefront-web uptime canary", { service: "storefront-web", family: "observability", signal: "synthetics", freshness: "stale", truth: "inferred" });
    // multi-cloud presence
    add("azure", "azure-edge", "azure_frontdoor_profile", "fd-storefronts", "Front Door · storefront edge", { service: "storefront-web" });
    add("azure", "azure-edge", "azure_monitor_workspace", "azmon-edge", "Azure Monitor · edge", { family: "observability", signal: "metrics" });
    add("gcp", "gcp-data", "gcp_bigquery_dataset", "bq-listings", "BigQuery · listings_analytics", { service: "svc-datax" });
    add("gcp", "gcp-data", "gcp_cloud_run_service", "cr-export", "Cloud Run · datax-export", { service: "svc-datax" });
    return out;
  }
  const cloudResources = buildCloudResources();

  // ===================================================== code intelligence (analyzer)
  // Dead-code findings = the /api/v0/code/dead-code analyzer output: unreferenced symbols
  // with 0 inbound CALLS/IMPORTS edges. kind ∈ function|class|const|export|route|file.
  const deadCode = [
    { id: "dc1", repo: "svc-classifieds", symbol: "legacySwigRenderer", file: "src/lib/render.js", line: 42, kind: "function", refs: 0, confidence: "exact", age: "live", loc: 58, reason: "No call sites — superseded by React SSR; still imports swig@1.4.2" },
    { id: "dc2", repo: "svc-classifieds", symbol: "AwsS3LegacyClient", file: "src/lib/s3-legacy.js", line: 11, kind: "class", refs: 0, confidence: "exact", age: "live", loc: 124, reason: "aws-sdk v2 client; no references since v3 migration of sibling modules" },
    { id: "dc3", repo: "svc-classifieds", symbol: "buildLegacyRequest", file: "src/lib/request-util.js", line: 7, kind: "function", refs: 0, confidence: "derived", age: "live", loc: 33, reason: "Wraps deprecated request@2.88.2; only reachable from removed routes" },
    { id: "dc4", repo: "svc-catalog", symbol: "experimentalRankV1", file: "src/services/rank.legacy.ts", line: 19, kind: "function", refs: 0, confidence: "exact", age: "live", loc: 91, reason: "Replaced by rankV2; export retained but never imported across the estate" },
    { id: "dc5", repo: "svc-catalog", symbol: "ES6FallbackMapper", file: "src/services/providers/es-fallback.ts", line: 23, kind: "class", refs: 0, confidence: "inferred", age: "5h", loc: 76, reason: "No CALLS edges observed; runtime trace coverage is partial here" },
    { id: "dc6", repo: "svc-platform", symbol: "composeLegacyClients", file: "src/services/compose.legacy.ts", line: 14, kind: "function", refs: 0, confidence: "exact", age: "live", loc: 47, reason: "Old client-composition path; superseded by acme-clients v19 bundle" },
    { id: "dc7", repo: "svc-platform", symbol: "DEFAULT_TIMEOUTS_V1", file: "src/config/timeouts.ts", line: 3, kind: "const", refs: 0, confidence: "derived", age: "live", loc: 12, reason: "Constant exported but shadowed by env-driven config" },
    { id: "dc8", repo: "svc-marketplace", symbol: "valkeyMigrationShim", file: "src/lib/valkey-shim.ts", line: 9, kind: "function", refs: 0, confidence: "exact", age: "live", loc: 64, reason: "Memcached→Valkey shim; migration complete, no callers remain" },
    { id: "dc9", repo: "svc-conversation", symbol: "twilioWebhookV1", file: "src/routes/webhook.legacy.ts", line: 21, kind: "route", refs: 0, confidence: "exact", age: "live", loc: 88, reason: "Unmounted route handler; replaced by /v2/webhook" },
    { id: "dc10", repo: "svc-forex", symbol: "staticRateTable", file: "src/lib/static-rates.ts", line: 5, kind: "const", refs: 0, confidence: "derived", age: "live", loc: 140, reason: "Hardcoded rate fallback table; no longer referenced after live FX feed" },
    { id: "dc11", repo: "svc-saved-search", symbol: "dynamoScanAll", file: "src/services/scan.ts", line: 31, kind: "function", refs: 0, confidence: "inferred", age: "5h", loc: 41, reason: "Full-table scan helper; reachability inferred — deploy evidence stale" },
    { id: "dc12", repo: "svc-classifieds", symbol: "OldSalesforceMapper", file: "src/services/sf-map.legacy.js", line: 16, kind: "class", refs: 0, confidence: "exact", age: "live", loc: 203, reason: "Pre-jsforce mapping; entire module unreferenced" },
    { id: "dc13", repo: "svc-external-search", symbol: "partnerFeedV1", file: "src/routes/partner.legacy.ts", line: 12, kind: "route", refs: 0, confidence: "derived", age: "live", loc: 72, reason: "Syndication v1 endpoint; partners migrated to v2" },
    { id: "dc14", repo: "storefront-web", symbol: "ssrCacheWarmer", file: "src/server/warm.legacy.ts", line: 8, kind: "function", refs: 0, confidence: "inferred", age: "2d", loc: 55, reason: "Cache pre-warmer; no invocation found in current entrypoints" },
    { id: "dc15", repo: "svc-taxonomy", symbol: "taxonomySeedV1", file: "src/data/seed.legacy.ts", line: 4, kind: "const", refs: 0, confidence: "exact", age: "live", loc: 318, reason: "One-time seed data committed to source; never imported at runtime" },
    { id: "dc16", repo: "svc-classifieds", symbol: "renderXmlSitemapLegacy", file: "src/routes/sitemap.legacy.js", line: 27, kind: "route", refs: 0, confidence: "derived", age: "live", loc: 96, reason: "Superseded by job-sitemaps-generator" }
  ];

  window.ESHU = {
    ENV, lang, collectorKinds, collectors, services, vulns, findings,
    relationships, layerColor, kindStyle, graph, nodeDetail, metrics, runtime,
    sev, statusColor, truthColor, freshColor,
    org: "demo",
    cloudAccounts, cloudResources, resourceFamily, cloudFamilies, signalKinds, deadCode,
    argocdApps, servicesById, buildEstateGraph,
    util: { mulberry32, series, fseries }
  };
})();
