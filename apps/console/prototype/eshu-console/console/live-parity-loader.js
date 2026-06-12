/* Extends the standalone prototype live loader with newer console surfaces. */
(function () {
  if (!window.ESHU || typeof window.ESHU.loadLive !== "function") return;

  const baseLoadLive = window.ESHU.loadLive;
  const OBSERVABILITY_PROVIDERS = ["grafana", "prometheus", "loki", "tempo"];
  const METRIC_SERIES = [
    ["ingestRate", "ingest_rate"],
    ["queueDepth", "queue_depth"],
    ["deadLetters", "dead_letters"],
    ["graphNodes", "graph_nodes"],
    ["graphEdges", "graph_edges"],
    ["queryP50", "query_p50"],
    ["queryP95", "query_p95"],
    ["queryP99", "query_p99"]
  ];

  function str(value) {
    return typeof value === "string" ? value : "";
  }
  function num(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : 0;
  }
  function truthLevel(env) {
    return env && env.truth && env.truth.level ? env.truth.level : "exact";
  }
  function envelopeErrorMessage(error) {
    if (!error) return "";
    if (typeof error === "string") return error;
    if (typeof error !== "object") return "api error";
    return str(error.message) || str(error.code) || "api error";
  }
  function apiData(env) {
    const message = envelopeErrorMessage(env && env.error);
    if (message) throw new Error(message);
    return env && env.data ? env.data : {};
  }
  async function section(out, key, fn) {
    try {
      const value = await fn();
      if (value !== undefined && value !== null) {
        Object.assign(out, value);
        out.prov[key] = "live";
      } else {
        out.prov[key] = "empty";
      }
    } catch (e) {
      out.prov[key] = "error:" + ((e && e.message) || "failed");
    }
  }

  function baseClient(client) {
    const wrapped = Object.create(client);
    wrapped.get = async function get(path) {
      if (path.indexOf("/api/v0/repositories/by-language?limit=100&offset=0") === 0) {
        return client.get("/api/v0/repositories/language-inventory?limit=100&offset=0");
      }
      if (path.indexOf("/api/v0/observability/coverage/correlations?limit=200") === 0) {
        return { data: { correlations: [] } };
      }
      return client.get(path);
    };
    wrapped.post = async function post(path, body) {
      if (path.indexOf("/api/v0/code/imports/investigate") === 0 ||
          path.indexOf("/api/v0/code/call-graph/metrics") === 0) {
        return { data: { dependencies: [], modules: [], package_imports: [], cycles: [], results: [] } };
      }
      return client.post(path, body);
    };
    return wrapped;
  }

  function mapImage(row, env) {
    const repository = str(row.repository);
    const tag = str(row.tag);
    const imageBase = [str(row.registry), repository].filter(Boolean).join("/");
    return {
      id: str(row.id) || str(row.digest),
      digest: str(row.digest),
      registry: str(row.registry),
      repository,
      name: str(row.name) || repository,
      tag,
      image: imageBase ? imageBase + (tag ? ":" + tag : "") : str(row.digest),
      mediaType: str(row.media_type),
      artifactType: str(row.artifact_type),
      sizeBytes: num(row.size_bytes) || null,
      sourceSystem: str(row.source_system),
      truth: truthLevel(env)
    };
  }

  function mapIac(row, env) {
    return {
      id: str(row.id),
      name: str(row.name) || str(row.resource_name) || str(row.id),
      kind: str(row.type) || str(row.kind) || "resource",
      ownerId: str(row.resource_service),
      owner: str(row.resource_service) || "shared platform",
      source: str(row.relative_path) ? str(row.relative_path) : "Terraform state",
      account: str(row.provider),
      region: str(row.module),
      truth: truthLevel(env)
    };
  }

  function mapDependency(row) {
    return {
      id: str(row.edge_id) || [str(row.anchor_package), str(row.related_package)].join("->"),
      direction: row.direction === "reverse" ? "reverse" : "forward",
      anchorPackage: str(row.anchor_package) || str(row.anchor_package_id) || "package",
      anchorPackageId: str(row.anchor_package_id),
      declaringVersion: str(row.declaring_version),
      relatedPackage: str(row.related_package) || str(row.related_package_id) || "dependency",
      relatedPackageId: str(row.related_package_id),
      ecosystem: str(row.related_ecosystem),
      range: str(row.dependency_range),
      dependencyType: str(row.dependency_type),
      optional: row.optional === true
    };
  }

  function mapSbomBucket(bucket) {
    return {
      id: str(bucket.value),
      value: str(bucket.value),
      dimension: str(bucket.dimension) || "subject_digest",
      count: num(bucket.count)
    };
  }

  function severityFromCvss(score) {
    if (score >= 9) return "critical";
    if (score >= 7) return "high";
    if (score >= 4) return "medium";
    return "low";
  }

  function mapAdvisory(row) {
    const cvss = num(row.cvss_score);
    const cve = str(row.cve_id);
    const ghsa = str(row.ghsa_id);
    return {
      id: str(row.advisory_key) || str(row.canonical_id) || cve || ghsa,
      cve,
      ghsa,
      severity: str(row.severity_label).toLowerCase() || severityFromCvss(cvss),
      cvss,
      kev: Boolean(row.kev),
      ecosystems: Array.isArray(row.ecosystems) ? row.ecosystems : [],
      packageIds: Array.isArray(row.package_ids) ? row.package_ids : [],
      publishedAt: str(row.published_at)
    };
  }

  function mapLanguage(row) {
    return {
      label: str(row.language) || str(row.name),
      value: num(row.repository_count) || num(row.count) || num(row.file_count)
    };
  }

  function cloudResourceFamily(resourceType) {
    const type = str(resourceType).toLowerCase();
    if (type.indexOf("iam") >= 0 || type.indexOf("role") >= 0 || type.indexOf("policy") >= 0) return "identity";
    if (type.indexOf("s3") >= 0 || type.indexOf("rds") >= 0 || type.indexOf("dynamo") >= 0 ||
        type.indexOf("elasticache") >= 0 || type.indexOf("opensearch") >= 0 || type.indexOf("storage") >= 0) return "storage";
    if (type.indexOf("vpc") >= 0 || type.indexOf("subnet") >= 0 || type.indexOf("security_group") >= 0 ||
        type.indexOf("gateway") >= 0 || type.indexOf("route") >= 0 || type.indexOf("network") >= 0) return "networking";
    if (type.indexOf("eks") >= 0 || type.indexOf("ecs") >= 0 || type.indexOf("lambda") >= 0 ||
        type.indexOf("apigateway") >= 0 || type.indexOf("compute") >= 0 || type.indexOf("instance") >= 0) return "compute";
    if (type.indexOf("cloudwatch") >= 0 || type.indexOf("grafana") >= 0 || type.indexOf("log") >= 0 ||
        type.indexOf("alarm") >= 0 || type.indexOf("monitor") >= 0) return "observability";
    return "other";
  }

  function cloudResourceName(uid, resourceType) {
    const clean = str(uid);
    if (!clean) return str(resourceType) || "cloud resource";
    const parts = clean.split(":").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : clean;
  }

  function cloudInventoryEvidence(evidence) {
    return {
      declared: Boolean(evidence && evidence.declared),
      applied: Boolean(evidence && evidence.applied),
      observed: Boolean(evidence && evidence.observed)
    };
  }

  function mapCloudInventoryRow(row) {
    const uid = str(row.cloud_resource_uid);
    const evidence = cloudInventoryEvidence(row.evidence || {});
    return {
      uid,
      provider: str(row.provider),
      resourceType: str(row.resource_type),
      origin: str(row.management_origin),
      scope: str(row.scope_id),
      sourceState: str(row.source_state),
      evidence
    };
  }

  function cloudInventoryResource(row, env) {
    return {
      uid: row.uid,
      provider: row.provider || "unknown",
      account: row.scope || row.provider || "unknown",
      region: "",
      type: row.resourceType || "cloud_resource",
      family: cloudResourceFamily(row.resourceType),
      name: cloudResourceName(row.uid, row.resourceType),
      resourceId: row.uid,
      ref: row.uid,
      service: null,
      tf: row.origin === "declared" || row.evidence.declared === true,
      truth: row.sourceState || truthLevel(env),
      freshness: "fresh",
      signal: null,
      sourceState: row.sourceState,
      managementOrigin: row.origin
    };
  }
  function cloudInventoryAccounts(rows, existing, resources) {
    const accounts = {};
    (existing || []).forEach((row) => { if (row && row.id) accounts[row.id] = row; });
    (resources || []).forEach((row) => { const id = row && row.account; if (id && !accounts[id]) accounts[id] = { id, provider: row.provider || "unknown", label: id, account: id, region: row.region || "", env: "live" }; });
    rows.forEach((row) => {
      const id = row.scope || row.provider || "unknown";
      if (!accounts[id]) {
        accounts[id] = {
          id,
          provider: row.provider || "unknown",
          label: id,
          account: id,
          region: "",
          env: row.origin || "inventory"
        };
      }
    });
    return Object.keys(accounts).map((key) => accounts[key]);
  }

  function mergeCloudResources(existing, canonical) {
    const seen = {};
    (existing || []).forEach((row) => { if (row && row.uid) seen[row.uid] = true; });
    return (existing || []).concat(canonical.filter((row) => row.uid && !seen[row.uid]));
  }
  function mapMetricPoints(env) {
    const points = apiData(env).points || [];
    return points.map((point) => num(point.v)).filter((value) => Number.isFinite(value));
  }

  async function loadMetricSeries(client) {
    const metrics = {};
    const errors = [];
    for (const pair of METRIC_SERIES) {
      const key = pair[0];
      const metric = pair[1];
      try {
        const env = await client.get("/api/v0/metrics/timeseries?metric=" + metric + "&window=24h&step=30m");
        const values = mapMetricPoints(env);
        if (values.length) metrics[key] = values;
      } catch (e) {
        errors.push((e && e.message) || "failed");
      }
    }
    if (Object.keys(metrics).length) return metrics;
    if (errors.length) throw new Error(errors[0]);
    return null;
  }

  function coverageState(row) {
    const status = str(row.coverage_status).toLowerCase();
    if (status === "gap") return "gap";
    if (status === "covered") return "covered";
    const outcome = str(row.outcome).toLowerCase();
    if (outcome === "exact" || outcome === "derived") return "covered";
    if (outcome === "stale") return "partial";
    return "gap";
  }

  function coverageSignal(row) {
    const sig = str(row.coverage_signal);
    const map = {
      alarm: "alerts",
      dashboard: "dashboards",
      scrape_target: "metrics",
      rule: "alerts",
      log_signal: "logs",
      trace_signal: "traces"
    };
    return map[sig] || sig;
  }

  function mapCoverageRow(row, provider) {
    const status = str(row.coverage_status) || "unknown";
    return {
      id: str(row.correlation_id) || [provider, coverageSignal(row), str(row.observability_object_ref), str(row.target_service_ref)].join(":"),
      provider: str(row.provider) || provider,
      signal: coverageSignal(row) || "unknown",
      object: str(row.observability_object_ref) || str(row.observability_resource_uid),
      target: str(row.target_service_ref) || str(row.target_uid),
      resourceClass: str(row.resource_class),
      sourceKind: str(row.source_kind),
      freshness: str(row.freshness_state),
      status,
      covered: status.toLowerCase() === "covered",
      reason: str(row.reason)
    };
  }

  async function loadObservabilityCoverage(client) {
    const coverage = {};
    const rows = [];
    const providerResults = [];
    for (const provider of OBSERVABILITY_PROVIDERS) {
      try {
        const env = await client.get("/api/v0/observability/coverage/correlations?provider=" + provider + "&limit=200");
        const data = apiData(env);
        const recs = (data.correlations || data.results) || [];
        recs.forEach((row) => {
          const ref = str(row.target_service_ref) || str(row.target_uid);
          const sig = coverageSignal(row);
          if (!ref || !sig) return;
          coverage[ref] = coverage[ref] || {};
          if (coverage[ref][sig]) return;
          coverage[ref][sig] = {
            state: coverageState(row),
            ref: str(row.observability_object_ref) || str(row.observability_resource_uid),
            freshness: str(row.freshness_state)
          };
        });
        recs.forEach((row) => rows.push(mapCoverageRow(row, provider)));
        providerResults.push({ provider, source: recs.length ? "live" : "empty", error: "" });
      } catch (e) {
        providerResults.push({ provider, source: "unavailable", error: (e && e.message) || "failed" });
      }
    }
    const byId = {};
    rows.forEach((row) => { if (!byId[row.id]) byId[row.id] = row; });
    const uniqueRows = Object.keys(byId).map((key) => byId[key]);
    const signals = {};
    uniqueRows.forEach((row) => { signals[row.signal] = (signals[row.signal] || 0) + 1; });
    const providers = providerResults.map((result) => {
      const owned = uniqueRows.filter((row) => row.provider === result.provider);
      return {
        provider: result.provider,
        total: owned.length,
        covered: owned.filter((row) => row.covered).length,
        gaps: owned.filter((row) => !row.covered).length,
        source: result.source,
        error: result.error
      };
    });
    return {
      coverage,
      snapshot: {
        rows: uniqueRows,
        providers,
        signals: Object.keys(signals).map((signal) => ({ signal, count: signals[signal] })).sort((a, b) => b.count - a.count),
        source: uniqueRows.length ? "live" : providerResults.some((p) => p.source === "unavailable") ? "unavailable" : "empty"
      }
    };
  }

  window.ESHU.loadLive = async function loadLiveWithParity(client) {
    const out = await baseLoadLive(baseClient(client));
    out.prov = out.prov || {};

    await section(out, "deadCode", async () => {
      return window.ESHU_LIVE_PARITY.loadDeadCode(client);
    });

    await section(out, "langInventory", async () => {
      const env = await client.get("/api/v0/repositories/language-inventory?limit=100&offset=0");
      const rows = (apiData(env).languages || []).map(mapLanguage).filter((row) => row.label);
      return rows.length ? { langInventory: rows } : null;
    });

    await section(out, "cloudInventory", async () => {
      const env = await client.get("/api/v0/cloud/inventory?limit=50");
      const data = apiData(env);
      const rows = ((data.resources) || []).map(mapCloudInventoryRow).filter((row) => row.uid);
      if (!rows.length) return null;
      const resources = rows.map((row) => cloudInventoryResource(row, env));
      const accounts = cloudInventoryAccounts(rows, out.cloudAccounts, out.cloudResources);
      const cloudAccounts = accounts.length ? accounts : out.cloudAccounts;
      return {
        cloudInventory: {
          count: typeof data.count === "number" ? data.count : rows.length,
          rows,
          truncated: Boolean(data.truncated),
          nextCursor: str(data.next_cursor)
        },
        cloudFamilies: Object.assign({}, window.ESHU.cloudFamilies || {}, out.cloudFamilies || {}, { other: { label: "Other", color: "#8b5cf6", icon: "box" } }),
        cloudResources: mergeCloudResources(out.cloudResources, resources),
        cloudAccounts,
        runtime: Object.assign({}, out.runtime || {}, { cloudResources: typeof data.count === "number" ? data.count : rows.length })
      };
    });

    await section(out, "imageInventory", async () => {
      const env = await client.get("/api/v0/images?limit=50&offset=0");
      const rows = (apiData(env).images || []).map((row) => mapImage(row, env)).filter((row) => row.id);
      return rows.length ? { imageInventory: rows } : null;
    });

    await section(out, "iacParityRows", async () => {
      const env = await client.get("/api/v0/iac/resources?limit=200");
      const rows = (apiData(env).resources || []).map((row) => mapIac(row, env)).filter((row) => row.id);
      return rows.length ? { iacParityRows: rows } : null;
    });

    await section(out, "sbomInventory", async () => {
      const countEnv = await client.get("/api/v0/supply-chain/sbom-attestations/attachments/count");
      const invEnv = await client.get("/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=subject_digest&limit=50&offset=0");
      const countData = apiData(countEnv);
      const invData = apiData(invEnv);
      const buckets = (invData.buckets || []).map(mapSbomBucket).filter((row) => row.id);
      return {
        sbomSummary: {
          total: num(countData.total_attachments),
          byStatus: countData.by_attachment_status || {},
          byArtifactKind: countData.by_artifact_kind || {},
          truth: truthLevel(countEnv)
        },
        sbomInventory: {
          groupBy: str(invData.group_by) || "subject_digest",
          buckets,
          truncated: Boolean(invData.truncated)
        }
      };
    });

    await section(out, "advisoryCatalog", async () => {
      const env = await client.get("/api/v0/supply-chain/advisories?limit=50");
      const rows = (apiData(env).advisories || []).map(mapAdvisory).filter((row) => row.id);
      return rows.length ? { advisoryCatalog: rows } : null;
    });

    await section(out, "dependencyInventory", async () => {
      const env = await client.get("/api/v0/dependencies?direction=forward&limit=50");
      const rows = (apiData(env).dependencies || []).map(mapDependency).filter((row) => row.id);
      return rows.length ? { dependencyInventory: rows } : null;
    });

    await section(out, "obsCoverage", async () => {
      const result = await loadObservabilityCoverage(client);
      return result ? { obsCoverage: result.coverage, obsCoverageSnapshot: result.snapshot } : null;
    });

    await section(out, "metrics", async () => {
      const series = await loadMetricSeries(client);
      return series ? { metrics: Object.assign({}, out.metrics || {}, series) } : null;
    });

    return out;
  };
})();
