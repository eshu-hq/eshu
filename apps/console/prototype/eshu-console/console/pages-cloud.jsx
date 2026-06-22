/* Eshu Console — Cloud inventory + Observability coverage surfaces.
   Grounded in the real model: canonical :CloudResource nodes (multi-cloud, keyed by
   uid = hash(account, region, resource_type, resource_id)) and the observability
   coverage-correlation read model (which service has metrics/logs/traces/alerts; gaps).
   Exports to window. Loaded after drill.jsx. */
const { useEffect: useEffectC, useState: useStateC, useMemo: useMemoC } = React;

const PROVIDER_META = {
  aws: { label: "AWS", color: "#ff9d2e" },
  azure: { label: "Azure", color: "#4f8cff" },
  gcp: { label: "GCP", color: "#22d3ee" }
};

function cloudEnvelopeData(response) {
  if (response && response.error) throw new Error(response.error.message || response.error.code || "api error");
  return response && response.data && response.error !== undefined ? response.data : response;
}

function cloudText(value) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function cloudEvidenceFlags(evidence) {
  const flags = [];
  if (evidence && evidence.declared) flags.push("declared");
  if (evidence && evidence.applied) flags.push("applied");
  if (evidence && evidence.observed) flags.push("observed");
  return flags;
}

async function loadCanonicalCloudInventory(client) {
  const data = cloudEnvelopeData(await client.get("/api/v0/cloud/inventory?limit=50")) || {};
  return {
    count: typeof data.count === "number" ? data.count : (data.resources || []).length,
    rows: (data.resources || []).map((row) => ({
      uid: cloudText(row.cloud_resource_uid),
      provider: cloudText(row.provider),
      resourceType: cloudText(row.resource_type),
      origin: cloudText(row.management_origin),
      scope: cloudText(row.scope_id),
      sourceState: cloudText(row.source_state),
      evidence: cloudEvidenceFlags(row.evidence || {})
    })).filter((row) => row.uid)
  };
}

/* ---- network topology graph for an account: Terraform → VPC → cluster/NAT → services → datastores */
function buildCloudNetwork(D, accountId) {
  const res = D.cloudResources.filter((r) => r.account === accountId);
  const acc = D.cloudAccounts.find((a) => a.id === accountId) || {};
  const mk = (r, col) => ({ id: r.uid, kind: CLOUD_FAM_KIND[r.family] || "aws", label: r.name, sub: cloudResLabel(r.type), col, _res: r });
  const nodes = [], edges = [], seen = new Set();
  const push = (n) => { if (!seen.has(n.id)) { seen.add(n.id); nodes.push(n); } };
  const t = (tt) => res.find((r) => r.type === tt);
  const tfId = "tf:" + accountId; push({ id: tfId, kind: "tf", label: "Terraform state", sub: "tfstate · " + (acc.env || ""), col: 0 });
  const vpc = t("aws_ec2_vpc"), cluster = t("aws_eks_cluster"), nat = t("aws_vpc_nat_gateway");
  if (vpc) { push(Object.assign(mk(vpc, 1), { hero: true })); edges.push({ s: vpc.uid, t: tfId, verb: "DECLARED_BY", layer: "infra" }); }
  if (nat) { push(mk(nat, 1)); if (vpc) edges.push({ s: nat.uid, t: vpc.uid, verb: "ROUTES_TO", layer: "infra" }); }
  if (cluster) { push(mk(cluster, 2)); if (vpc) edges.push({ s: cluster.uid, t: vpc.uid, verb: "RUNS_IN", layer: "runtime" }); edges.push({ s: cluster.uid, t: tfId, verb: "DECLARED_BY", layer: "infra" }); }
  const svcIds = Array.from(new Set(res.filter((r) => r.service).map((r) => r.service)));
  svcIds.forEach((sid) => {
    const svc = D.servicesById[sid]; if (!svc) return;
    const snid = "svc:" + sid;
    push({ id: snid, kind: svc.kind === "lib" ? "library" : "service", label: svc.name, sub: (svc.tier || "") + " · " + svc.system, col: 3 });
    if (cluster) edges.push({ s: snid, t: cluster.uid, verb: "RUNS_IN", layer: "runtime" });
    const sg = res.find((r) => r.service === sid && r.type === "aws_security_group");
    if (sg) { push(mk(sg, 2)); edges.push({ s: snid, t: sg.uid, verb: "SECURED_BY", layer: "infra" }); if (vpc) edges.push({ s: sg.uid, t: vpc.uid, verb: "ATTACHED_TO", layer: "infra" }); }
    res.filter((r) => r.service === sid && r.family === "storage").forEach((ds) => { push(mk(ds, 4)); edges.push({ s: snid, t: ds.uid, verb: "STORES_IN", layer: "infra" }); });
  });
  if (!edges.length && res.length) return buildCanonicalCloudNetwork(D, accountId, res);
  return { nodes, edges };
}

function buildCanonicalCloudNetwork(D, accountId, res) {
  const acc = D.cloudAccounts.find((a) => a.id === accountId) || {};
  const nodes = [], edges = [], seen = new Set();
  const push = (n) => { if (!seen.has(n.id)) { seen.add(n.id); nodes.push(n); } };
  const rootId = "scope:" + accountId;
  push({ id: rootId, kind: acc.provider || "aws", label: acc.label || accountId, sub: "canonical inventory scope", col: 0, hero: true });
  res.forEach((r) => {
    const origin = r.managementOrigin || (r.tf ? "declared" : "observed");
    const originId = "origin:" + accountId + ":" + origin;
    const family = r.family || "other";
    const familyId = "family:" + accountId + ":" + family;
    const fm = D.cloudFamilies[family] || { label: family, color: "var(--muted)" };
    push({ id: originId, kind: origin === "declared" ? "tf" : "aws", label: origin || "inventory", sub: "management origin", col: 1 });
    push({ id: familyId, kind: CLOUD_FAM_KIND[family] || "aws", label: fm.label, sub: "resource family", col: 2 });
    push({ id: r.uid, kind: CLOUD_FAM_KIND[family] || "aws", label: r.name || r.type || r.uid, sub: cloudResLabel(r.type), col: 3, _res: r });
    edges.push({ s: rootId, t: originId, verb: "CONTAINS", layer: "infra" });
    edges.push({ s: originId, t: familyId, verb: "GROUPS", layer: "infra" });
    edges.push({ s: familyId, t: r.uid, verb: "HAS_RESOURCE", layer: "infra" });
  });
  return { nodes, edges };
}

/* ---- a single service's cloud network: IAM, security group, VPC/cluster, datastores, all declared by Terraform */
function buildServiceNetwork(D, s) {
  if (!s) return { nodes: [], edges: [] };
  const res = D.cloudResources.filter((r) => r.service === s.id);
  const acc = res[0] ? res[0].account : (s.envs && s.envs.includes("acme-prod") ? "aws-prod" : "aws-qa");
  const shared = D.cloudResources.filter((r) => r.account === acc && ["aws_ec2_vpc", "aws_eks_cluster", "aws_vpc_nat_gateway"].includes(r.type));
  const mk = (r) => ({ id: r.uid, kind: CLOUD_FAM_KIND[r.family] || "aws", label: r.name, sub: cloudResLabel(r.type), _res: r });
  const nodes = [], edges = [], seen = new Set();
  const push = (n) => { if (!seen.has(n.id)) { seen.add(n.id); nodes.push(n); } };
  const snid = "svc:" + s.id;
  push({ id: snid, kind: s.kind === "lib" ? "library" : "service", label: s.name, sub: (s.tier || "") + " · " + s.system, hero: true });
  const tfId = "tf:" + acc; push({ id: tfId, kind: "tf", label: "Terraform", sub: "IaC state" });
  const vpc = shared.find((r) => r.type === "aws_ec2_vpc"), cluster = shared.find((r) => r.type === "aws_eks_cluster"), nat = shared.find((r) => r.type === "aws_vpc_nat_gateway");
  if (cluster) { push(mk(cluster)); edges.push({ s: snid, t: cluster.uid, verb: "RUNS_IN", layer: "runtime" }); if (vpc) edges.push({ s: cluster.uid, t: vpc.uid, verb: "RUNS_IN", layer: "runtime" }); }
  if (vpc) { push(mk(vpc)); edges.push({ s: vpc.uid, t: tfId, verb: "DECLARED_BY", layer: "infra" }); }
  if (nat) { push(mk(nat)); if (vpc) edges.push({ s: nat.uid, t: vpc.uid, verb: "ROUTES_TO", layer: "infra" }); }
  const iam = res.find((r) => r.type === "aws_iam_role");
  if (iam) { push(mk(iam)); edges.push({ s: snid, t: iam.uid, verb: "ASSUMES_ROLE", layer: "infra" }); edges.push({ s: iam.uid, t: tfId, verb: "DECLARED_BY", layer: "infra" }); }
  const sg = res.find((r) => r.type === "aws_security_group");
  if (sg) { push(mk(sg)); edges.push({ s: snid, t: sg.uid, verb: "SECURED_BY", layer: "infra" }); if (vpc) edges.push({ s: sg.uid, t: vpc.uid, verb: "ATTACHED_TO", layer: "infra" }); }
  res.filter((r) => r.family === "storage").forEach((ds) => { push(mk(ds)); edges.push({ s: snid, t: ds.uid, verb: "STORES_IN", layer: "infra" }); });
  return { nodes, edges };
}

/* ===================================================================== CLOUD */
function Cloud({ data, client, onOpenService, onOpenNode }) {
  const D = data || ESHU;
  const all = D.cloudResources;
  const [provider, setProvider] = useStateC("all");
  const [fam, setFam] = useStateC("all");
  const [q, setQ] = useStateC("");
  const [view, setView] = useStateC("network");
  const [inventory, setInventory] = useStateC(null);
  const [inventoryState, setInventoryState] = useStateC("demo");
  const networkAccounts = D.cloudAccounts;
  const [netAcct, setNetAcct] = useStateC(networkAccounts[0] ? networkAccounts[0].id : "aws-prod");
  const net = useMemoC(() => buildCloudNetwork(D, netAcct), [D, netAcct]);
  const famKeys = Object.keys(D.cloudFamilies);
  const providers = Array.from(new Set(all.map((r) => r.provider)));

  const rows = all.filter((r) =>
    (provider === "all" || r.provider === provider) &&
    (fam === "all" || r.family === fam) &&
    (q === "" || (r.name + r.type + r.account + (r.service || "")).toLowerCase().includes(q.toLowerCase()))
  );
  const tfPct = all.length ? Math.round((all.filter((r) => r.tf).length / all.length) * 100) : 0;
  const obsCount = all.filter((r) => r.family === "observability").length;
  const famCounts = famKeys.map((k) => ({ label: D.cloudFamilies[k].label, value: all.filter((r) => r.family === k).length, color: D.cloudFamilies[k].color }));

  useEffectC(() => {
    let cancelled = false;
    if (!client) { setInventory(null); setInventoryState("demo"); return; }
    setInventoryState("loading");
    loadCanonicalCloudInventory(client)
      .then((result) => {
        if (!cancelled) { setInventory(result); setInventoryState("live"); }
      })
      .catch((e) => {
        if (!cancelled) { setInventory({ count: 0, rows: [], error: (e && e.message) || "failed" }); setInventoryState("unavailable"); }
      });
    return () => { cancelled = true; };
  }, [client]);

  function openRes(res) {
    const { node, graph } = cloudResourceGraph(res, D);
    onOpenNode(node, graph);
  }

  return (
    <div className="page">
      <div className="page-intro"><h2>Cloud</h2><p>Every <span className="mono">:CloudResource</span> Eshu has materialised across AWS, Azure &amp; GCP — keyed by <span className="mono">account · region · resource_type</span>, joined to the services that use them and the Terraform that declares them. Select any resource to drill into its graph.</p></div>

      <div className="grid g-4">
        <StatTile label="Cloud resources" value={fmt(all.length)} color="var(--blue)" sub={"indexed of " + fmt(D.runtime.cloudResources) + " discovered"} />
        <StatTile label="Accounts" value={D.cloudAccounts.length} color="var(--ember)" sub={providers.map((p) => PROVIDER_META[p].label).join(" · ")} />
        <StatTile label="Terraform-managed" value={tfPct + "%"} color="var(--teal)" sub="declared by IaC state" />
        <StatTile label="Observability objects" value={obsCount} color="var(--ok, #22c55e)" sub="alarms · logs · traces · dashboards" onClick={() => { window.ESHU_ROUTES.setHash("observability"); }} cta="Coverage" />
      </div>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="Resources by family" sub="Across all accounts" glyph={<Icon.layers />}>
          <BarRows rows={famCounts.sort((a, b) => b.value - a.value)} />
        </Panel>
        <Panel title="Accounts" sub="Provider · region · environment" glyph={<Icon.cloud />}>
          <div className="acct-list">
            {D.cloudAccounts.map((a) => {
              const n = all.filter((r) => r.account === a.id).length;
              const pm = PROVIDER_META[a.provider] || { label: a.provider || "Provider", color: "var(--muted)" };
              return (
                <div className="acct-row" key={a.id}>
                  <span className="acct-prov" style={{ "--pc": pm.color }}><i />{pm.label}</span>
                  <span className="cell-stack" style={{ flex: 1, minWidth: 0 }}><span className="t-name" style={{ fontSize: ".84rem" }}>{a.label}</span><small className="mono">{a.account} · {a.region}</small></span>
                  <span className={"tag-tier tier-" + (a.env === "acme-prod" ? "1" : "2")}>{a.env}</span>
                  <span className="mono t-mut" style={{ fontSize: ".78rem", width: 36, textAlign: "right" }}>{n}</span>
                </div>
              );
            })}
          </div>
        </Panel>
      </div>

      <Panel className="flush mt" title="Canonical inventory" sub={client ? "GET /api/v0/cloud/inventory" : "Connect live to inspect canonical reducer inventory"} glyph={<Icon.cloud />}>
        {inventoryState === "loading" ? (
          <div className="conn-state" style={{ padding: 28 }}><div className="conn-spinner" aria-hidden /><p>Loading canonical cloud inventory...</p></div>
        ) : inventory && inventory.rows.length ? (
          <table className="tbl">
            <thead><tr><th>Resource UID</th><th>Provider</th><th>Type</th><th>Scope</th><th>State</th><th>Evidence</th></tr></thead>
            <tbody>{inventory.rows.map((row) => (
              <tr key={row.uid}>
                <td className="mono" style={{ fontSize: ".76rem" }}>{row.uid}</td>
                <td><Badge tone="neutral">{row.provider || "provider"}</Badge></td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{row.resourceType || "resource"}</td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{row.scope || row.origin || "scope pending"}</td>
                <td>{row.sourceState || "unknown"}</td>
                <td><div className="row wrap" style={{ gap: 5 }}>{row.evidence.length ? row.evidence.map((flag) => <Badge key={flag} tone="teal">{flag}</Badge>) : <Badge tone="neutral">none</Badge>}</div></td>
              </tr>
            ))}</tbody>
          </table>
        ) : (
          <p className="empty">{inventory && inventory.error ? "Canonical inventory unavailable: " + inventory.error : "No canonical inventory rows returned yet."}</p>
        )}
      </Panel>

      <div className="row mt" style={{ justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <div className="dep-toggle" style={{ margin: 0 }}>
          <button className={view === "network" ? "active" : ""} onClick={() => setView("network")}>Network</button>
          <button className={view === "table" ? "active" : ""} onClick={() => setView("table")}>Table</button>
        </div>
        {view === "network" ? (
          <div className="seg branch-seg"><Icon.cloud size={14} />{networkAccounts.map((a) => <button key={a.id} className={netAcct === a.id ? "active" : ""} onClick={() => setNetAcct(a.id)}>{a.label} · {a.env}</button>)}</div>
        ) : null}
      </div>

      {view === "network" ? (
        <Panel className="flush mt" title="Network topology" sub={"Terraform → VPC → cluster → services → datastores · " + (D.cloudAccounts.find((a) => a.id === netAcct) || {}).account + " · click any node to drill"} glyph={<Icon.branch />}>
          <GraphCanvas graph={net} data={D} layout="layered" height={580} onSelect={(n) => onOpenNode(n, net)} />
        </Panel>
      ) : (
      <>
      <div className="repo-toolbar mt">
        <div className="searchbox" style={{ minWidth: 240, height: 38, margin: 0, flex: 1 }}><Icon.search size={16} /><input placeholder="Find a resource, type or account…" value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <div className="seg">{["all"].concat(providers).map((p) => <button key={p} className={provider === p ? "active" : ""} onClick={() => setProvider(p)}>{p === "all" ? "All clouds" : PROVIDER_META[p].label}</button>)}</div>
      </div>
      <div className="explorer-filters" style={{ marginTop: 10 }}>
        <span className="row" style={{ gap: 7, color: "var(--subtle)", fontSize: ".78rem", fontWeight: 700, textTransform: "uppercase", letterSpacing: ".08em", marginRight: 4 }}><Icon.filter size={15} />Family</span>
        <button className={cx("layer-toggle", fam === "all" ? "on" : "off")} style={{ "--lc": "var(--muted)" }} onClick={() => setFam("all")}><span>All</span><span className="lt-n">{all.length}</span></button>
        {famKeys.map((k) => (
          <button key={k} className={cx("layer-toggle", fam === k ? "on" : "off")} style={{ "--lc": D.cloudFamilies[k].color }} onClick={() => setFam(fam === k ? "all" : k)}>
            <i style={{ background: D.cloudFamilies[k].color }} /><span>{D.cloudFamilies[k].label}</span><span className="lt-n">{all.filter((r) => r.family === k).length}</span>
          </button>
        ))}
      </div>

      <Panel className="flush mt" title={rows.length + " resources"} sub="Grouped by family · click a row to open it in the graph" glyph={<Icon.cloud />}>
        <table className="tbl cloud-tbl">
          <thead><tr><th>Resource</th><th>Type</th><th>Account · region</th><th>Service</th><th>IaC</th><th>Truth</th><th></th></tr></thead>
          <tbody>
            {famKeys.map((k) => {
              const list = rows.filter((r) => r.family === k);
              if (!list.length) return null;
              const fm = D.cloudFamilies[k];
              const FI = Icon[fm.icon] || Icon.box;
              return (
                <React.Fragment key={k}>
                  <tr className="group-row"><td colSpan={7}><span className="group-label" style={{ color: fm.color }}><FI size={13} /> {fm.label}</span><span className="group-meta">{list.length} {list.length === 1 ? "resource" : "resources"}</span></td></tr>
                  {list.map((r) => {
                    const acc = D.cloudAccounts.find((a) => a.id === r.account) || {};
                    const svc = r.service && D.servicesById[r.service];
                    return (
                      <tr key={r.uid} className="cloud-row" onClick={() => openRes(r)} style={{ cursor: "pointer" }}>
                        <td><span className="row" style={{ gap: 9 }}><span className="res-dot" style={{ background: fm.color }} /><span className="cell-stack"><span style={{ fontWeight: 600 }}>{r.name}</span><small className="mono">{r.uid}</small></span></span></td>
                        <td><span className="mono res-type">{r.type}</span></td>
                        <td className="cell-stack"><span className="t-mut" style={{ fontSize: ".8rem" }}>{acc.label}</span><small className="mono">{r.region}</small></td>
                        <td>{svc ? <button className="dep-chip" style={{ fontSize: ".72rem" }} onClick={(e) => { e.stopPropagation(); onOpenService(svc.id); }}>{svc.name}</button> : <span className="t-mut" style={{ fontSize: ".78rem" }}>—</span>}</td>
                        <td>{r.tf ? <Badge tone="teal">terraform</Badge> : <Badge tone="neutral">untracked</Badge>}</td>
                        <td><TruthChip level={r.truth} /></td>
                        <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
                      </tr>
                    );
                  })}
                </React.Fragment>
              );
            })}
            {rows.length === 0 ? <tr><td colSpan={7}><p className="empty">No resources match.</p></td></tr> : null}
          </tbody>
        </table>
      </Panel>
      </>
      )}
    </div>
  );
}

/* ============================================================= OBSERVABILITY */
/* coverage-correlation read model: per running service, which signals are present. */
function deriveObservability(D) {
  const running = D.services.filter((s) => s.kind !== "lib");
  const bySvc = {};
  D.cloudResources.forEach((r) => { if (r.service && r.signal) (bySvc[r.service] = bySvc[r.service] || []).push(r); });
  const SIGNALS = Object.keys(D.signalKinds);
  const rows = running.map((s) => {
    const res = bySvc[s.id] || [];
    const has = (sig) => res.find((r) => r.signal === sig);
    const tier = s.tier;
    const live = D.obsCoverage && (D.obsCoverage[s.name] || D.obsCoverage[s.id]);
    function status(sig) {
      if (live && live[sig]) return { state: live[sig].state, live: true };
      const r = has(sig);
      if (r) return { state: r.freshness === "stale" ? "partial" : "covered", res: r };
      if (sig === "metrics") return { state: tier === "tier-3" ? "partial" : "covered" };
      if (sig === "logs") return { state: s.freshness === "lagging" ? "partial" : "covered" };
      if (sig === "traces") return { state: tier === "tier-1" ? "covered" : tier === "tier-2" ? "partial" : "gap" };
      if (sig === "dashboards") return { state: tier === "tier-3" ? "gap" : "partial" };
      if (sig === "alerts") return { state: tier === "tier-3" ? "gap" : "covered" };
      return { state: "gap" }; // synthetics
    }
    const cov = {}; SIGNALS.forEach((sig) => cov[sig] = status(sig));
    const gaps = SIGNALS.filter((sig) => cov[sig].state === "gap").length;
    const score = SIGNALS.reduce((a, sig) => a + (cov[sig].state === "covered" ? 1 : cov[sig].state === "partial" ? 0.5 : 0), 0) / SIGNALS.length;
    return { svc: s, cov, gaps, score };
  });
  return { rows, SIGNALS };
}

const STATE_COLOR = { covered: "var(--teal)", partial: "var(--med)", gap: "var(--crit)" };
const STATE_GLYPH = { covered: "●", partial: "◐", gap: "○" };

function Observability({ data, onOpenService, onOpenNode, onOpenCollector }) {
  const D = data || ESHU;
  const liveSnap = D.obsCoverageSnapshot || null;
  const liveRows = liveSnap && liveSnap.rows ? liveSnap.rows : [];
  const liveProviders = liveSnap && liveSnap.providers ? liveSnap.providers : [];
  const { rows, SIGNALS } = useMemoC(() => deriveObservability(D), [D]);
  const obsCollectors = D.collectors.filter((c) => COLLECTOR_DOMAIN.Observability.includes(c.kind));

  const fullCovered = rows.filter((r) => r.gaps === 0).length;
  const withGaps = rows.filter((r) => r.gaps > 0).length;
  const totalGaps = rows.reduce((a, r) => a + r.gaps, 0);
  const staleSources = obsCollectors.filter((c) => c.status !== "healthy").length;

  // per-signal rollup
  const perSignal = SIGNALS.map((sig) => {
    const c = { covered: 0, partial: 0, gap: 0 };
    rows.forEach((r) => { c[r.cov[sig].state]++; });
    return { sig, meta: D.signalKinds[sig], c, pct: Math.round((c.covered / rows.length) * 100) };
  });
  const worst = rows.slice().sort((a, b) => b.gaps - a.gaps).filter((r) => r.gaps > 0).slice(0, 8);

  function openCell(r, sig) {
    const cell = r.cov[sig];
    if (cell.res) { const { node, graph } = cloudResourceGraph(cell.res, D); onOpenNode(node, graph); }
    else onOpenService(r.svc.id);
  }

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro"><h2>Observability</h2><p>Coverage correlations for grafana, prometheus/mimir, loki, and tempo — which workloads emit <strong>metrics, logs, traces, dashboards, alerts</strong> and <strong>synthetics</strong>, and where the gaps are. Click any cell to drill to the signal or the service.</p></div>

      <div className="grid g-4">
        <StatTile label="Services monitored" value={rows.length} color="var(--teal)" sub="running workloads" />
        <StatTile label="Full coverage" value={fullCovered + "/" + rows.length} color="var(--teal)" sub="all six signals present" />
        <StatTile label="Coverage gaps" value={totalGaps} color="var(--crit)" sub={withGaps + " services with \u2265 1 gap"} />
        <StatTile label="Source health" value={(obsCollectors.length - staleSources) + "/" + obsCollectors.length} color={staleSources ? "var(--med)" : "var(--teal)"} sub={staleSources ? staleSources + " degraded / stale" : "all healthy"} onClick={() => { window.ESHU_ROUTES.setHash("admin"); }} cta="Operations" />
      </div>

      <Panel className="mt" title="Signal sources" sub={obsCollectors.length + " observability collectors feeding the graph"} glyph={<Icon.cloud />}>
        <div className="signal-source-grid">
          {obsCollectors.map((c) => {
            const k = D.collectorKinds[c.kind];
            return (
              <button type="button" className="signal-source" key={c.instance} onClick={() => onOpenCollector && onOpenCollector(c)}>
                <CollectorGlyph kind={c.kind} size={28} />
                <span className="cell-stack" style={{ minWidth: 0 }}><span style={{ fontWeight: 600, fontSize: ".84rem" }}>{k.label}</span><small className="mono">{fmt(c.facts)} facts</small></span>
                <span className="status-pill" style={{ color: D.statusColor[c.status] }}><i style={{ background: D.statusColor[c.status] }} />{c.status}</span>
              </button>
            );
          })}
        </div>
      </Panel>

      {liveSnap ? (
        <Panel className="mt" title="Provider coverage" sub={"GET /api/v0/observability/coverage/correlations · " + liveSnap.source} glyph={<Icon.pulse />}>
          <div className="signal-source-grid">
            {liveProviders.map((p) => (
              <div className="signal-source" key={p.provider}>
                <span className="cglyph" style={{ width: 28, height: 28 }}>{p.provider.slice(0, 1).toUpperCase()}</span>
                <span className="cell-stack" style={{ minWidth: 0 }}>
                  <span style={{ fontWeight: 600, fontSize: ".84rem" }}>{p.provider}</span>
                  <small className="mono">{p.covered}/{p.total} covered · {p.gaps} gaps</small>
                </span>
                <span className={"status-pill " + (p.source === "unavailable" ? "bad" : "")}><i />{p.source}</span>
              </div>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel className="flush mt" title="Coverage matrix" sub="Per service × signal — ● covered · ◐ partial · ○ gap" glyph={<Icon.spark />}>
        <div className="cov-scroll">
          <table className="tbl cov-matrix">
            <thead>
              <tr>
                <th>Service</th>
                {SIGNALS.map((sig) => <th key={sig} className="cov-col"><span style={{ color: D.signalKinds[sig].color }}>{D.signalKinds[sig].label}</span><small>{perSignal.find((p) => p.sig === sig).pct}%</small></th>)}
                <th>Score</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice().sort((a, b) => b.score - a.score).map((r) => (
                <tr key={r.svc.id} className="cov-row">
                  <td className="cell-stack" onClick={() => onOpenService(r.svc.id)} style={{ cursor: "pointer" }}><span className="t-name" style={{ fontSize: ".82rem" }}>{r.svc.name}</span><small>{r.svc.tier} · {r.svc.system}</small></td>
                  {SIGNALS.map((sig) => {
                    const cell = r.cov[sig];
                    return (
                      <td key={sig} className="cov-cell" onClick={() => openCell(r, sig)} title={D.signalKinds[sig].label + ": " + cell.state + (cell.res ? " · " + cell.res.name : "")}>
                        <span className="cov-mark" style={{ color: STATE_COLOR[cell.state] }}>{STATE_GLYPH[cell.state]}</span>
                      </td>
                    );
                  })}
                  <td><span className="cov-score"><span className="cov-score-bar"><i style={{ width: Math.round(r.score * 100) + "%", background: r.score > 0.75 ? "var(--teal)" : r.score > 0.5 ? "var(--med)" : "var(--crit)" }} /></span><span className="mono" style={{ fontSize: ".72rem", color: "var(--muted)" }}>{Math.round(r.score * 100)}%</span></span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>

      <div className="grid g-2 mt">
        <Panel title="Coverage by signal" sub="Share of services emitting each signal" glyph={<Icon.pulse />}>
          <div className="sigcov-list">
            {perSignal.map((p) => (
              <div className="sigcov-row" key={p.sig}>
                <span className="sigcov-label" style={{ color: p.meta.color }}>{p.meta.label}</span>
                <div className="sigcov-bar">
                  {p.c.covered ? <span style={{ width: (p.c.covered / rows.length * 100) + "%", background: "var(--teal)" }} title={p.c.covered + " covered"} /> : null}
                  {p.c.partial ? <span style={{ width: (p.c.partial / rows.length * 100) + "%", background: "var(--med)" }} title={p.c.partial + " partial"} /> : null}
                  {p.c.gap ? <span style={{ width: (p.c.gap / rows.length * 100) + "%", background: "var(--crit)" }} title={p.c.gap + " gap"} /> : null}
                </div>
                <span className="sigcov-meta mono">{p.meta.sources}</span>
              </div>
            ))}
          </div>
        </Panel>
        <Panel className="flush" title="Biggest coverage gaps" sub="Services missing the most signals" glyph={<Icon.findings />}>
          <table className="tbl">
            <thead><tr><th>Service</th><th>Tier</th><th>Missing</th><th>Coverage</th><th></th></tr></thead>
            <tbody>
              {worst.map((r) => (
                <tr key={r.svc.id} onClick={() => onOpenService(r.svc.id)} style={{ cursor: "pointer" }}>
                  <td className="t-name" style={{ fontSize: ".82rem" }}>{r.svc.name}</td>
                  <td><span className={"tag-tier tier-" + r.svc.tier}>{r.svc.tier}</span></td>
                  <td><div className="row wrap" style={{ gap: 5 }}>{SIGNALS.filter((sig) => r.cov[sig].state === "gap").map((sig) => <span key={sig} className="gap-chip" style={{ "--gc": D.signalKinds[sig].color }}>{D.signalKinds[sig].label}</span>)}</div></td>
                  <td className="mono" style={{ fontSize: ".8rem", color: r.score > 0.5 ? "var(--med)" : "var(--crit)" }}>{Math.round(r.score * 100)}%</td>
                  <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
                </tr>
              ))}
              {worst.length === 0 ? <tr><td colSpan={5}><p className="empty">Every monitored service has full signal coverage.</p></td></tr> : null}
            </tbody>
          </table>
        </Panel>
      </div>

      {liveSnap ? (
        <Panel className="flush mt" title="Coverage correlations" sub={liveRows.length + " reducer-owned rows"} glyph={<Icon.db />}>
          <table className="tbl">
            <thead><tr><th>Provider</th><th>Signal</th><th>Object</th><th>Target</th><th>Freshness</th><th>Status</th></tr></thead>
            <tbody>
              {liveRows.slice(0, 80).map((row) => (
                <tr key={row.id}>
                  <td className="t-name">{row.provider}</td>
                  <td className="mono t-mut" style={{ fontSize: ".78rem" }}>{row.signal}</td>
                  <td className="t-mut" style={{ fontSize: ".78rem" }}>{row.object || "—"}</td>
                  <td className="t-mut" style={{ fontSize: ".78rem" }}>{row.target || "—"}</td>
                  <td className="mono t-mut" style={{ fontSize: ".74rem" }}>{row.freshness || "—"}</td>
                  <td>{row.covered ? <Badge tone="teal">{row.status}</Badge> : <Badge tone="crit">{row.status}</Badge>}</td>
                </tr>
              ))}
              {liveRows.length === 0 ? <tr><td colSpan={6}><p className="empty">No live coverage correlations from this source.</p></td></tr> : null}
            </tbody>
          </table>
        </Panel>
      ) : null}
    </div>
  );
}

Object.assign(window, { Cloud, Observability, deriveObservability, buildCloudNetwork, buildServiceNetwork });
