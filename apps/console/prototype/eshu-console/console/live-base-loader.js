/* Eshu Console prototype: base live API hydration. */
(function () {
  "use strict";
  if (!window.ESHU) return;

  function chipTruth(level) {
    return level === "fallback" ? "inferred" : (level || "exact");
  }

  function chipFresh(state) {
    return state === "building" ? "lagging" : state === "unavailable" ? "stale" : (state || "fresh");
  }

  async function loadLive(client) {
    const E = window.ESHU;
    const out = { prov: {}, truth: {} };
    async function section(key, fn) {
      try {
        const v = await fn();
        if (v !== undefined && v !== null) {
          out[key] = v;
          out.prov[key] = "live";
        } else {
          out.prov[key] = "empty";
        }
      } catch (e) {
        out.prov[key] = "error:" + (e && e.message || "failed");
      }
    }

    await section("runtime", async () => {
      const rt = Object.assign({}, E.runtime);
      try {
        const eco = (await client.get("/api/v0/ecosystem/overview")).data || {};
        if (eco.repository_count != null) rt.repos = eco.repository_count;
        if (eco.workload_count != null) rt.workloads = eco.workload_count;
        if (eco.platform_count != null) rt.platforms = eco.platform_count;
        if (eco.instance_count != null) rt.instances = eco.instance_count;
      } catch (e) {}
      const env = await client.get("/api/v0/index-status");
      const st = env.data || {};
      const q = st.queue || {};
      rt.indexStatus = st.status || rt.indexStatus;
      if (st.repository_count != null) rt.repos = st.repository_count;
      rt.queueOutstanding = q.outstanding != null ? q.outstanding : (q.pending || 0);
      rt.inFlight = q.in_flight || 0;
      rt.deadLetters = q.dead_letter || 0;
      rt.succeeded = q.succeeded || 0;
      if (env.truth) {
        rt.profile = env.truth.profile || rt.profile;
        out.truth.runtime = env.truth;
      }
      rt._live = true;
      return rt;
    });

    await section("services", async () => {
      const env = await client.get("/api/v0/catalog?limit=2000&offset=0");
      const c = env.data || {};
      const list = [];
      (c.services || []).concat(c.workloads || []).forEach((w) => {
        list.push({
          id: w.id || w.name,
          name: w.name || w.id,
          kind: "api",
          repo: w.repo_name || w.repo_id || "",
          host: "",
          version: "—",
          lang: "ts",
          tier: "tier-2",
          owner: "—",
          system: w.kind || "Service",
          envs: w.environments || [],
          image: null,
          port: null,
          deps: [],
          stores: [],
          crit: 0,
          high: 0,
          med: 0,
          low: 0,
          incidents: 0,
          workItems: 0,
          freshness: chipFresh(w.materialization_status === "graph" ? "fresh" : "building"),
          truth: chipTruth((env.truth && env.truth.level) || "exact"),
          coverage: 1,
          calls: 0,
          callers: 0,
          blastRadius: 0,
          story: "Live catalog entry from the Eshu API" + (w.repo_name ? " · defined by " + w.repo_name : "") + "."
        });
      });
      (c.repositories || []).forEach((r) => {
        if (list.find((s) => s.name === (r.name || r.id))) return;
        list.push({ id: r.id || r.name, name: r.name || r.id, kind: "lib", repo: r.repo_slug || r.local_path || r.name, host: "", version: "—", lang: "ts", tier: "lib", owner: "—", system: "Repository", envs: [], image: null, port: null, deps: [], stores: [], crit: 0, high: 0, med: 0, low: 0, incidents: 0, workItems: 0, freshness: "fresh", truth: chipTruth((env.truth && env.truth.level) || "exact"), coverage: 1, calls: 0, callers: 0, blastRadius: 0, story: "Indexed repository from the live Eshu API." });
      });
      return list.length ? list : null;
    });

    await section("langInventory", async () => {
      const env = await client.get("/api/v0/repositories/by-language?limit=100&offset=0");
      const d = env.data || {};
      const arr = d.languages || d.results || (Array.isArray(d) ? d : []);
      const rows = arr.map((x) => ({ label: x.language || x.name, value: x.count || x.repository_count || x.repositories || 0 })).filter((r) => r.label);
      return rows.length ? rows : null;
    });

    await section("collectors", async () => {
      const env = await client.get("/api/v0/status/ingesters");
      const d = env.data || {};
      const arr = d.ingesters || d.results || (Array.isArray(d) ? d : []);
      const rows = arr.map((g) => ({
        kind: (g.kind || g.ingester || "git").toLowerCase().replace(/[^a-z_]/g, "_"),
        instance: g.id || g.ingester || g.name || "ingester",
        status: (g.state || g.status || "healthy") === "healthy" ? "healthy" : (g.state === "degraded" ? "degraded" : "stale"),
        facts: g.fact_count || g.facts || 0,
        scopes: g.scope_count || g.scopes || 0,
        lastRun: g.last_run || g.updated_at || "—",
        latencyMs: g.latency_ms || 0,
        freshness: chipFresh(g.freshness || "fresh"),
        cadence: g.cadence || "—",
        note: g.note || g.detail || ""
      }));
      return rows.length ? rows : null;
    });

    await section("findings", async () => {
      const env = await client.post("/api/v0/code/dead-code", { limit: 25 });
      const d = env.data || {};
      const lvl = chipTruth((env.truth && env.truth.level) || "derived");
      const rows = (d.results || []).map((r, i) => ({
        id: "live-dc-" + i,
        type: "Dead code",
        severity: "low",
        entity: r.repo_name || r.repo_id || "repository",
        title: "Unreferenced symbol " + (r.name || "candidate"),
        detail: (r.file_path || "unknown") + (r.classification ? " · " + r.classification : ""),
        truth: lvl,
        source: "code",
        age: "live"
      }));
      return rows.length ? rows : null;
    });

    await section("vulns", async () => {
      const env = await client.get("/api/v0/supply-chain/impact/findings?limit=50");
      const d = env.data || {};
      const arr = d.findings || d.results || [];
      const sevMap = { critical: "critical", high: "high", medium: "medium", moderate: "medium", low: "low" };
      const rows = arr.map((v) => ({
        cve: v.advisory_id || v.cve || v.id || "ADVISORY",
        pkg: v.package || v.package_name || v.subject || "—",
        version: v.version || v.affected_version || "",
        ecosystem: v.ecosystem || "npm",
        severity: sevMap[(v.severity || "").toLowerCase()] || "medium",
        cvss: v.cvss || v.cvss_score || 0,
        epss: v.epss || 0,
        kev: !!(v.kev || v.known_exploited),
        fixAvailable: !!(v.fixed_version || v.fix_available),
        fixed: v.fixed_version || null,
        services: v.affected_services || v.services || (v.repository_id ? [v.repository_id] : []),
        firstSeen: "live",
        title: v.title || v.summary || v.advisory_id || "Advisory",
        source: v.source || "supply-chain",
        prov: chipTruth((env.truth && env.truth.level) || "exact") === "inferred" ? "inferred" : "derived"
      }));
      return rows.length ? rows : null;
    });

    await section("cloudResources", async () => {
      const env = await client.get("/api/v0/cloud/resources?limit=200");
      const d = env.data || {};
      const arr = d.resources || d.results || (Array.isArray(d) ? d : []);
      const obsSignal = { aws_cloudwatch_alarm: "alerts", aws_cloudwatch_dashboard: "dashboards", aws_cloudwatch_logs_log_group: "logs", aws_xray_sampling_rule: "traces", aws_amp_workspace: "metrics", aws_grafana_workspace: "dashboards", aws_synthetics_canary: "synthetics", azure_monitor_workspace: "metrics" };
      const lvl = chipTruth((env.truth && env.truth.level) || "exact");
      const rows = arr.map((r) => ({
        uid: r.id || r.cloud_resource_uid || "",
        provider: (r.provider || r.collector_kind || "aws").replace(/_cloud$/, ""),
        account: r.account_id || r.scope_id || "",
        region: r.region || "",
        type: r.resource_type || "aws_resource",
        family: E.resourceFamily[r.resource_type] || "compute",
        name: r.name || r.resource_id || r.id,
        resourceId: r.resource_id || "",
        ref: r.arn || r.id || "",
        service: r.service_name || null,
        tf: r.state ? /managed|declared|applied/.test(String(r.state)) : true,
        truth: lvl,
        freshness: "fresh",
        signal: obsSignal[r.resource_type] || null
      })).filter((r) => r.uid);
      return rows.length ? rows : null;
    });

    await section("deadCode", async () => {
      const env = await client.post("/api/v0/code/dead-code", { limit: 100 });
      const d = env.data || {};
      const lvl = chipTruth((env.truth && env.truth.level) || "derived");
      const repoNameById = {};
      (out.services || []).forEach((s) => { if (s.id && s.name) repoNameById[s.id] = s.name; if (s.repo && s.name) repoNameById[s.repo] = s.name; });
      const rows = (d.results || []).map((r, i) => ({
        id: "dc-" + i,
        entityId: r.entity_id || "",
        repoId: r.repo_id || "",
        repoName: r.repo_name || repoNameById[r.repo_id] || "",
        repo: r.repo_name || repoNameById[r.repo_id] || r.repo_id || "repository",
        symbol: r.name || "symbol",
        file: r.file_path || r.relative_path || "",
        line: r.line || r.start_line || 0,
        endLine: r.end_line || r.line || r.start_line || 0,
        kind: String(r.entity_kind || r.classification || "function").toLowerCase(),
        refs: r.reference_count || 0,
        confidence: lvl,
        age: "live",
        loc: r.loc || r.line_count || 0,
        reason: r.reason || r.classification || "No inbound CALLS / IMPORTS edges"
      }));
      return rows.length ? rows : null;
    });

    await section("codeImports", async () => {
      const svcs = out.services || E.services;
      const repoSvcs = svcs.filter((s) => s.repo).slice(0, 40);
      if (!repoSvcs.length) return null;
      const nameToId = {};
      svcs.forEach((s) => { nameToId[s.name] = s.id; });
      const map = {};
      for (const s of repoSvcs) {
        try {
          const mod = await client.post("/api/v0/code/imports/investigate", { repo_id: s.id, query_type: "module_dependencies", limit: 80 });
          const deps = (mod.data || {}).dependencies || (mod.data || {}).modules || [];
          const modEdges = deps.map((d) => ({ s: d.source_module || d.source || d.from || s.name, t: d.target_module || d.target || d.to || d.module || d.name })).filter((e) => e.s && e.t);
          let hubs = [];
          try { const cg = await client.post("/api/v0/code/call-graph/metrics", { repo_id: s.id, metric: "hub_functions", limit: 8 }); hubs = ((cg.data && (cg.data.hub_functions || cg.data.results)) || []).map((h) => ({ name: h.name || h.function || h.symbol || "fn", c: h.references || h.callers || h.degree || 0 })); } catch (e) {}
          let cycles = [];
          try { const cyc = await client.post("/api/v0/code/imports/investigate", { repo_id: s.id, query_type: "file_import_cycles", limit: 20 }); cycles = (cyc.data && cyc.data.cycles) || []; } catch (e) {}
          try {
            const pk = await client.post("/api/v0/code/imports/investigate", { repo_id: s.id, query_type: "package_imports", limit: 100 });
            const pkgs = (pk.data && (pk.data.dependencies || pk.data.modules || pk.data.package_imports)) || [];
            const dep = [];
            pkgs.forEach((p) => { const nm = String(p.package || p.name || p.target_module || "").replace(/^@[^/]+\//, ""); if (nameToId[nm] && nameToId[nm] !== s.id) dep.push(nameToId[nm]); });
            if (dep.length) s.deps = Array.from(new Set(dep));
          } catch (e) {}
          map[s.id] = { modEdges, hubs, cycles };
        } catch (e) {}
      }
      return Object.keys(map).length ? map : null;
    });

    await section("obsCoverage", async () => {
      const env = await client.get("/api/v0/observability/coverage/correlations?limit=200");
      const d = env.data || {};
      const rows = d.correlations || d.results || [];
      if (!rows.length) return null;
      const sigMap = { alarm: "alerts", dashboard: "dashboards", scrape_target: "metrics", rule: "alerts", log_signal: "logs", trace_signal: "traces" };
      const cov = {};
      rows.forEach((r) => {
        const ref = r.target_service_ref || r.target_uid;
        if (!ref) return;
        const sig = sigMap[r.coverage_signal];
        if (!sig) return;
        const state = r.coverage_status === "gap" ? "gap" : r.coverage_status === "covered" ? "covered" : (r.outcome === "exact" || r.outcome === "derived" ? "covered" : r.outcome === "stale" ? "partial" : "gap");
        (cov[ref] = cov[ref] || {})[sig] = { state, ref: r.observability_object_ref || r.observability_resource_uid, freshness: r.freshness_state };
      });
      return Object.keys(cov).length ? cov : null;
    });

    return out;
  }

  window.ESHU.loadLive = loadLive;
})();
