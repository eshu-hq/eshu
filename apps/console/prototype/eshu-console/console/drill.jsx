/* Eshu Console — universal drill-down: a dedicated drawer for EVERY node kind,
   path tracing between nodes, and a collector drawer. So nothing is a dead end.
   Loaded after graph-extras.jsx + pages-core.jsx (needs NodeInspector, GraphCanvas).
   Exports to window. */
const { useMemo: useMemoDr, useState: useStateDr } = React;

/* ---- map a graph node back to a real indexed service id (for the rich spotlight) */
function resolveServiceId(node, D) {
  if (!node) return null;
  if (D.servicesById && D.servicesById[node.id]) return node.id;          // estate graph
  const byName = D.services.find((s) => s.name === node.label);            // curated graph
  return byName ? byName.id : null;
}

/* ---- shortest path between two node ids (BFS over the undirected graph) */
function tracePath(graph, aId, bId) {
  if (!aId || !bId || aId === bId) return null;
  const adj = {};
  graph.edges.forEach((e) => {
    (adj[e.s] = adj[e.s] || []).push({ to: e.t, e });
    (adj[e.t] = adj[e.t] || []).push({ to: e.s, e });
  });
  const prev = { [aId]: null }; const q = [aId];
  while (q.length) {
    const cur = q.shift();
    if (cur === bId) break;
    (adj[cur] || []).forEach(({ to, e }) => {
      if (!(to in prev)) { prev[to] = { from: cur, e }; q.push(to); }
    });
  }
  if (!(bId in prev)) return null; // disconnected
  const nodeIds = new Set(); const edges = new Set(); const seq = [];
  let cur = bId;
  while (cur != null) {
    nodeIds.add(cur); seq.unshift(cur);
    const p = prev[cur];
    if (p) { edges.add(edgeKey(p.e)); cur = p.from; } else cur = null;
  }
  return { nodes: nodeIds, edges, seq, hops: seq.length - 1 };
}

/* ---- the immediate neighbourhood of a node as a small graph (for the drawer canvas) */
function nodeNeighborhood(node, graph) {
  const nb = new Set([node.id]);
  graph.edges.forEach((e) => { if (e.s === node.id) nb.add(e.t); if (e.t === node.id) nb.add(e.s); });
  const nodes = graph.nodes.filter((n) => nb.has(n.id)).map((n) => Object.assign({}, n, { hero: n.id === node.id }));
  const edges = graph.edges.filter((e) => nb.has(e.s) && nb.has(e.t));
  return { nodes, edges };
}

/* ---- pretty label for a resource_type token (aws_iam_role -> IAM Role) */
const RES_LABEL = {
  aws_iam_role: "IAM Role", aws_iam_policy: "IAM Policy", aws_iam_instance_profile: "Instance Profile", aws_eks_oidc_provider: "EKS OIDC Provider", aws_accessanalyzer_analyzer: "Access Analyzer",
  aws_ec2_vpc: "VPC", aws_vpc_nat_gateway: "NAT Gateway", aws_vpc_endpoint: "VPC Endpoint", aws_security_group: "Security Group", aws_apigateway_rest_api: "API Gateway",
  aws_eks_cluster: "EKS Cluster", aws_eks_nodegroup: "EKS Nodegroup", aws_ec2_instance: "EC2 Instance", aws_autoscaling_group: "Auto Scaling Group",
  aws_s3_bucket: "S3 Bucket", aws_dynamodb_table: "DynamoDB Table", aws_rds_db_instance: "RDS Instance", aws_elasticache_cluster: "ElastiCache Cluster", aws_opensearch_domain: "OpenSearch Domain", aws_redshift_cluster: "Redshift Cluster",
  aws_sqs_queue: "SQS Queue", aws_sns_topic: "SNS Topic", aws_eventbridge_event_bus: "EventBridge Bus",
  aws_cloudwatch_alarm: "CloudWatch Alarm", aws_cloudwatch_dashboard: "CloudWatch Dashboard", aws_cloudwatch_logs_log_group: "CloudWatch Log Group", aws_xray_sampling_rule: "X-Ray Sampling Rule", aws_amp_workspace: "Prometheus Workspace", aws_grafana_workspace: "Grafana Workspace", aws_synthetics_canary: "Synthetics Canary",
  azure_frontdoor_profile: "Front Door Profile", azure_monitor_workspace: "Azure Monitor", gcp_bigquery_dataset: "BigQuery Dataset", gcp_cloud_run_service: "Cloud Run Service"
};
function cloudResLabel(type) { return RES_LABEL[type] || type.replace(/^[a-z]+_/, "").replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()); }
const CLOUD_FAM_KIND = { identity: "aws", networking: "workload", compute: "service", storage: "datastore", messaging: "workitem", observability: "monitor" };

/* ---- a CloudResource as a graph node + its small neighbourhood (for openNode) */
function cloudResourceGraph(res, D) {
  const acc = (D.cloudAccounts || []).find((a) => a.id === res.account) || { label: res.account, account: res.account, region: res.region, provider: res.provider };
  const node = { id: res.uid, kind: CLOUD_FAM_KIND[res.family] || "aws", label: res.name, sub: cloudResLabel(res.type) + " · " + res.region, hero: true, truth: res.truth, _res: res };
  const nodes = [node];
  const edges = [];
  if (res.service && D.servicesById[res.service]) {
    const svc = D.servicesById[res.service];
    nodes.push({ id: svc.id, kind: svc.kind === "lib" ? "library" : svc.kind === "web" ? "service" : "service", label: svc.name, sub: (svc.tier || "") + " · " + svc.system });
    const verb = res.family === "identity" ? "ASSUMES_ROLE" : res.family === "storage" ? "STORES_IN" : res.family === "observability" ? ((D.signalKinds[res.signal] || {}).verb || "EMITS_METRICS") : "DEPENDS_ON";
    const layer = res.family === "observability" ? "ops" : res.family === "compute" ? "runtime" : "infra";
    edges.push({ s: svc.id, t: res.uid, verb, layer });
  }
  nodes.push({ id: "acct:" + res.account, kind: "env", label: acc.label, sub: res.provider.toUpperCase() + " · " + acc.region });
  edges.push({ s: res.uid, t: "acct:" + res.account, verb: "RUNS_IN", layer: "runtime" });
  if (res.tf) { nodes.push({ id: "tf:" + res.uid, kind: "tf", label: "Terraform", sub: "IaC state" }); edges.push({ s: res.uid, t: "tf:" + res.uid, verb: "DECLARED_BY", layer: "infra" }); }
  return { node, graph: { nodes, edges } };
}

/* ---- kind-specific fact rows ([label, value, color?]) synthesised from the node + estate */
function kindFacts(node, graph, D) {
  if (node._res) {
    const res = node._res;
    const acc = (D.cloudAccounts || []).find((a) => a.id === res.account) || {};
    const svc = res.service && D.servicesById[res.service];
    const F = [["Provider", res.provider.toUpperCase()], ["Account", (acc.label || res.account) + " · " + (acc.account || "")], ["Region", res.region], ["Type", res.type, (D.cloudFamilies[res.family] || {}).color], ["IaC", res.tf ? "Terraform-managed" : "untracked", res.tf ? "var(--teal)" : "var(--med)"], ["Service", svc ? svc.name : "—"]];
    if (res.signal) F.push(["Signal", (D.signalKinds[res.signal] || {}).label || res.signal, (D.signalKinds[res.signal] || {}).color]);
    return F;
  }
  const conns = nodeConnections(node, graph);
  const out = (node.id || node.label || "").replace(/^[a-z]+:/, "");
  const verbsOut = conns.filter((c) => c.dir === "out").map((c) => c.edge.verb);
  const verbsIn = conns.filter((c) => c.dir === "in").map((c) => c.edge.verb);
  const F = [];
  switch (node.kind) {
    case "repo":
      F.push(["Host", (node.sub || "git").split(" · ")[0]], ["Default branch", "main"], ["Edges", conns.length], ["Builds", verbsOut.includes("BUILDS") ? "image + client" : "—"]);
      break;
    case "image":
      F.push(["Registry", "ECR · us-east-1"], ["Base", "node-api-base:1.0.0"], ["Deploys to", verbsOut.includes("DEPLOYS_FROM") ? "workload" : "—"], ["Scanned", verbsOut.includes("AFFECTED_BY") ? "CVE found" : "clean", verbsOut.includes("AFFECTED_BY") ? "var(--crit)" : "var(--teal)"]);
      break;
    case "client":
      F.push(["Registry", "npm · @acme (private)"], ["Version", (node.sub || "").split("· ").pop() || "—"], ["Importers", verbsIn.filter((v) => v === "IMPORTS").length || conns.length], ["Provenance", "build attestation"]);
      break;
    case "workload":
      F.push(["Kind", "Deployment"], ["Namespace", "api-node"], ["Orchestrator", "EKS"], ["Environments", conns.filter((c) => c.neighborKind === "env").length || "—"]);
      break;
    case "env":
      F.push(["Cluster", "eks-" + out], ["Region", "us-east-1"], ["Sync", "ArgoCD"], ["Workloads here", conns.length]);
      break;
    case "datastore":
      F.push(["Engine", node.label], ["Access", "read / write"], ["Discovered via", "config + IRSA grants"], ["Consumers", verbsIn.filter((v) => v === "STORES_IN").length || conns.length]);
      break;
    case "tf":
      F.push(["IaC", "Terraform state"], ["Module", "iac-eks-* / terraform-stack-*"], ["Mechanism", "Crossplane XIRSARole"], ["Declares", verbsOut.includes("DECLARED_BY") ? "IAM role" : "—"]);
      break;
    case "aws":
      F.push(["Service", "IAM"], ["Mechanism", "IRSA"], ["Scope", "least-privilege read"], ["Declared by", verbsIn.includes("DECLARED_BY") ? "Terraform" : "—"]);
      break;
    case "edge":
      F.push(["Provider", "Cloudflare"], ["Controls", "WAF · cache · routes"], ["Portals fronted", 13], ["Collector", "cloudflare · 5m poll"]);
      break;
    case "monitor": {
      const sig = /loki/i.test(node.label) ? "logs" : /datadog|otel|telemetry|trace/i.test(node.label) ? "traces" : "metrics";
      F.push(["Signal", sig], ["Collector", node.label], ["Correlation", sig === "logs" ? "trace ⋈ log by id" : sig === "traces" ? "spans → service map" : "RED + USE panels"], ["Sources", conns.length]);
      break;
    }
    case "workitem":
      F.push(["Tracker", "Jira · ACME-NODE"], ["State", (node.sub || "").split("· ").pop() || "open"], ["Linked to", verbsIn.includes("TRACKED_BY") ? "repo / change" : "service"], ["Edges", conns.length]);
      break;
    case "incident":
      F.push(["Source", "PagerDuty"], ["Correlation", "change-event window"], ["Edges", conns.length]);
      break;
    case "vuln": {
      const v = (D.vulns || []).find((x) => x.cve === node.label);
      if (v) F.push(["CVSS", v.cvss, D.sev[v.severity]], ["EPSS", Math.round((v.epss || 0) * 100) + "%"], ["KEV", v.kev ? "listed" : "no", v.kev ? "var(--crit)" : null], ["Fix", v.fixAvailable ? v.fixed : "none", v.fixAvailable ? "var(--teal)" : "var(--crit)"]);
      else F.push(["Type", "Vulnerability"], ["Joined via", "SBOM ⋈ advisory"], ["Edges", conns.length]);
      break;
    }
    default:
      F.push(["Kind", (ESHU.kindStyle[node.kind] || {}).label || node.kind], ["Edges", conns.length]);
  }
  return F;
}

/* ---- which cross-page destinations make sense for this node */
function nodeCrossLinks(node, D) {
  const links = [];
  if (node._res) {
    if (node._res.service && D.servicesById[node._res.service]) links.push({ label: "Service spotlight", icon: "box", to: { kind: "service", id: node._res.service } });
    links.push({ label: "Cloud inventory", icon: "cloud", to: { kind: "route", route: "cloud" } });
    if (node._res.family === "observability") links.push({ label: "Observability coverage", icon: "spark", to: { kind: "route", route: "observability" } });
    return links;
  }
  const sid = resolveServiceId(node, D);
  if (sid) links.push({ label: "Service spotlight", icon: "box", to: { kind: "service", id: sid } });
  if (sid && D.servicesById[sid] && D.servicesById[sid].repo) links.push({ label: "Open repository", icon: "catalog", to: { kind: "route", route: "repos" } });
  if (node.kind === "repo") links.push({ label: "Browse in Repositories", icon: "catalog", to: { kind: "route", route: "repos" } });
  if (node.kind === "vuln") { const v = (D.vulns || []).find((x) => x.cve === node.label); if (v) links.push({ label: "Open in Vulnerabilities", icon: "vuln", to: { kind: "vuln", cve: v.cve } }); }
  if (node.kind === "monitor" || node.kind === "edge") links.push({ label: "Collector health", icon: "cloud", to: { kind: "route", route: "admin" } });
  if (node.kind === "tf" || node.kind === "aws") links.push({ label: "Cloud resources", icon: "cloud", to: { kind: "route", route: "admin" } });
  return links;
}

/* =================================================================== NODE DRAWER */
/* A dedicated detail drawer for ANY node kind. Service/lib route to the rich spotlight. */
function NodeDrawer({ node, graph, data, onClose, onOpenNode, onOpenService, onOpenVuln, onGo }) {
  const D = data || ESHU;
  const G = graph || D.graph;
  const res = node._res;
  const ks = res ? { color: (D.cloudFamilies[res.family] || {}).color || "#999", label: cloudResLabel(res.type) } : (ESHU.kindStyle[node.kind] || { color: "#999", label: node.kind });
  const det = (D.nodeDetail || {})[node.id];
  const facts = useMemoDr(() => kindFacts(node, G, D), [node, G, D]);
  const conns = useMemoDr(() => nodeConnections(node, G), [node, G]);
  const hood = useMemoDr(() => nodeNeighborhood(node, G), [node, G]);
  const links = useMemoDr(() => nodeCrossLinks(node, D), [node, D]);
  const truth = det ? det.truth : node.truth || "exact";
  const fresh = det ? det.freshness : (res ? res.freshness : "fresh");
  const resEvidence = res ? [
    cloudResLabel(res.type) + " · uid " + res.uid,
    res.ref,
    "account " + res.account + " · region " + res.region,
    res.tf ? "DECLARED_BY Terraform state" : "no IaC source — console-managed drift",
    res.service ? "linked to service " + (D.servicesById[res.service] || {}).name : "no service binding indexed"
  ] : null;
  const openOriginal = (n) => { const o = G.nodes.find((x) => x.id === n.id) || n; onOpenNode(o, G); };
  function follow(to) {
    if (to.kind === "service") onOpenService(to.id);
    else if (to.kind === "vuln") onOpenVuln && onOpenVuln(to.cve);
    else if (to.kind === "route") onGo && onGo(to.route);
  }
  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label={node.label + " detail"}>
        <div className="drawer-head">
          <div className="row" style={{ gap: 12, minWidth: 0 }}>
            <span className="cglyph" style={{ width: 34, height: 34, color: ks.color, borderColor: ks.color, fontSize: ".58rem" }}>{(ks.label || "?").slice(0, 2)}</span>
            <div style={{ minWidth: 0 }}>
              <div className="insp-kind" style={{ color: ks.color }}>{ks.label}</div>
              <div className="row" style={{ gap: 8, minWidth: 0 }}><strong style={{ fontFamily: "var(--mono)", fontSize: "1rem", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{node.label}</strong></div>
            </div>
          </div>
          <button className="drawer-close" onClick={onClose} aria-label="Close"><Icon.close size={16} /></button>
        </div>
        <div className="drawer-body">
          {node.sub ? <div className="t-mut mono" style={{ fontSize: ".8rem", marginTop: -4 }}>{node.sub}</div> : null}
          <div className="row wrap" style={{ gap: 10 }}><TruthChip level={truth} /><FreshDot state={fresh} /><span className="node-layer-chip" style={{ "--ec": ks.color }}><i />{conns.length} {conns.length === 1 ? "edge" : "edges"}</span></div>

          {links.length ? (
            <div className="insp-actions">
              {links.map((l, i) => { const I = Icon[l.icon] || Icon.arrow; return <button key={i} className={cx("btn-ghost", l.to.kind === "service" && "active")} onClick={() => follow(l.to)}><I size={14} /> {l.label} →</button>; })}
            </div>
          ) : null}

          <div>
            <div className="section-label">Facts on this node</div>
            <div className="meta-dl" style={{ gridTemplateColumns: "repeat(2,1fr)" }}>
              {facts.map(([k, v, c], i) => <div key={i}><dt>{k}</dt><dd style={Object.assign({ fontSize: "1rem" }, c ? { color: c } : null)}>{v}</dd></div>)}
            </div>
          </div>

          <div>
            <div className="section-label">Typed evidence</div>
            <div className="insp-evi">{(det ? det.evidence : resEvidence ? resEvidence : ["" + node.label + " resolved from the canonical NornicDB graph", "kind: " + node.kind]).map((e, i) => <div className="insp-evi-row" key={i}>{e}</div>)}</div>
          </div>

          <div>
            <div className="section-label">Neighbourhood · click any node to drill</div>
            <div className="node-hood">
              <GraphCanvas graph={hood} data={D} layout="radial" height={250} showLabels
                onSelect={(n) => { if (n.id !== node.id) openOriginal(n); }} selectedId={node.id} />
            </div>
          </div>

          <div>
            <div className="section-label">Connections · {conns.length}</div>
            {conns.length ? (
              <div className="conn-list">
                {conns.map((c, i) => {
                  const col = ESHU.layerColor[c.edge.layer] || "var(--teal)";
                  return (
                    <div className="conn-row" key={i} style={{ "--ec": col }}>
                      <span className="conn-main" style={{ cursor: "default" }}><span className="conn-dir" style={{ color: col }}>{c.dir === "out" ? "→" : "←"}</span><span className="conn-verb" style={{ color: col }}>{c.edge.verb}</span></span>
                      <button type="button" className="conn-neighbor" onClick={() => openOriginal({ id: c.neighborId, label: c.neighborLabel, kind: c.neighborKind })} title="Open this node">
                        <i style={{ background: (ESHU.kindStyle[c.neighborKind] || {}).color || "var(--subtle)" }} />{c.neighborLabel}<Icon.arrow size={13} style={{ marginLeft: "auto", color: "var(--subtle)" }} />
                      </button>
                    </div>
                  );
                })}
              </div>
            ) : <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>No edges from this node in the current view.</p>}
          </div>

          <p className="t-mut" style={{ fontSize: ".72rem", borderTop: "1px solid var(--line)", paddingTop: 14, margin: 0, lineHeight: 1.5 }}>
            <span className="mono" style={{ color: "var(--subtle)" }}>provenance</span> · this {ks.label.toLowerCase()} and its edges are resolved from the NornicDB graph. Every neighbour and relationship above is itself clickable — drill until you reach the evidence you need.
          </p>
        </div>
      </aside>
    </>
  );
}

/* ============================================================== COLLECTOR DRAWER */
/* clicking a collector (Operations) opens what it produces + where it lands in the graph */
const COLLECTOR_PRODUCES = {
  git: { verbs: ["IMPORTS", "PUBLISHES", "TRACKED_BY"], kinds: ["Repository", "npm Client"], node: "repo:catalog" },
  package_registry: { verbs: ["PUBLISHES"], kinds: ["npm Client"], node: "client:catalog" },
  oci_registry: { verbs: ["BUILDS", "DEPLOYS_FROM"], kinds: ["Image"], node: "img:catalog" },
  sbom_attestation: { verbs: ["AFFECTED_BY"], kinds: ["Image"], node: "img:catalog" },
  kubernetes: { verbs: ["RUNS_AS", "RUNS_IN", "DISCOVERS_CONFIG_IN"], kinds: ["Workload", "Environment"], node: "wl:catalog" },
  aws: { verbs: ["DECLARED_BY", "ASSUMES_ROLE"], kinds: ["AWS Resource"], node: "aws:role" },
  terraform_state: { verbs: ["DECLARED_BY"], kinds: ["Terraform"], node: "tf:irsa" },
  cloudflare: { verbs: ["FRONTED_BY"], kinds: ["Edge / CDN"], node: "edge:cf" },
  vulnerability_intelligence: { verbs: ["AFFECTED_BY"], kinds: ["Vulnerability"], node: "vuln:base" },
  security_alert: { verbs: ["AFFECTED_BY"], kinds: ["Vulnerability"], node: "vuln:base" },
  jira: { verbs: ["TRACKED_BY"], kinds: ["Work item"], node: "wi:catalog" },
  pagerduty: { verbs: ["OBSERVED_INCIDENT"], kinds: ["Incident"], node: null },
  prometheus_mimir: { verbs: ["EMITS_METRICS"], kinds: ["Telemetry"], node: "mon:metrics" },
  cloudwatch: { verbs: ["EMITS_METRICS"], kinds: ["Telemetry"], node: "mon:metrics" },
  otel_traces: { verbs: ["TRACED_BY", "DEPENDS_ON"], kinds: ["Telemetry"], node: "mon:traces" },
  grafana_loki: { verbs: ["LOGS_TO"], kinds: ["Telemetry"], node: "mon:logs" },
  datadog: { verbs: ["TRACED_BY"], kinds: ["Telemetry"], node: "mon:apm" },
  grafana_synthetic: { verbs: ["EMITS_METRICS"], kinds: ["Telemetry"], node: null }
};
const COLLECTOR_DOMAIN = {
  "Source & build": ["git", "package_registry", "oci_registry", "sbom_attestation"],
  "Cloud & infra": ["aws", "terraform_state", "kubernetes", "cloudflare"],
  "Security": ["vulnerability_intelligence", "security_alert"],
  "Observability": ["prometheus_mimir", "cloudwatch", "otel_traces", "grafana_loki", "datadog", "grafana_synthetic"],
  "Ops & delivery": ["jira", "pagerduty"]
};
function collectorDomain(kind) {
  for (const d in COLLECTOR_DOMAIN) if (COLLECTOR_DOMAIN[d].includes(kind)) return d;
  return "Other";
}

function CollectorDrawer({ collector, data, onClose, onGo, onOpenNode }) {
  const D = data || ESHU;
  const c = collector;
  const k = ESHU.collectorKinds[c.kind] || { label: c.kind, color: "#999" };
  const prod = COLLECTOR_PRODUCES[c.kind] || { verbs: [], kinds: [] };
  const statusC = { healthy: "var(--teal)", degraded: "var(--med)", stale: "var(--crit)" }[c.status] || "var(--muted)";
  const graphNode = prod.node ? (D.graph.nodes.find((n) => n.id === prod.node)) : null;
  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label={k.label + " collector"}>
        <div className="drawer-head">
          <div className="row" style={{ gap: 12, minWidth: 0 }}>
            <CollectorGlyph kind={c.kind} size={34} />
            <div style={{ minWidth: 0 }}>
              <div className="insp-kind" style={{ color: k.color }}>Collector · {collectorDomain(c.kind)}</div>
              <strong style={{ fontFamily: "var(--mono)", fontSize: "1rem" }}>{k.label}</strong>
            </div>
          </div>
          <button className="drawer-close" onClick={onClose} aria-label="Close"><Icon.close size={16} /></button>
        </div>
        <div className="drawer-body">
          <div className="t-mut mono" style={{ fontSize: ".8rem", marginTop: -4 }}>{c.instance}</div>
          <div className="row wrap" style={{ gap: 10 }}>
            <span className="status-chip" style={{ "--sc": statusC }}><i />{c.status}</span>
            <FreshDot state={c.freshness} />
            <span className="t-mut mono" style={{ fontSize: ".74rem" }}>{c.cadence}</span>
          </div>
          {c.note ? <p style={{ color: "var(--muted)", lineHeight: 1.55, margin: 0, fontSize: ".88rem" }}>{c.note}</p> : null}

          <div className="meta-dl" style={{ gridTemplateColumns: "repeat(2,1fr)" }}>
            <div><dt>Facts</dt><dd style={{ fontSize: "1.05rem" }}>{fmt(c.facts)}</dd></div>
            <div><dt>Scopes</dt><dd style={{ fontSize: "1.05rem" }}>{c.scopes}</dd></div>
            <div><dt>Latency</dt><dd style={{ fontSize: "1.05rem", color: c.latencyMs > 2000 ? "var(--med)" : "var(--bone)" }}>{c.latencyMs ? c.latencyMs + "ms" : "—"}</dd></div>
            <div><dt>Last run</dt><dd style={{ fontSize: "1.05rem" }}>{c.lastRun}</dd></div>
          </div>

          <div>
            <div className="section-label">Produces these edges</div>
            <div className="row wrap" style={{ gap: 7 }}>
              {prod.verbs.length ? prod.verbs.map((v) => { const rel = (D.relationships || []).find((r) => r.verb === v); const col = rel ? D.layerColor[rel.layer] : "var(--teal)"; return <span key={v} className="verb-chip" style={{ "--ec": col }}><i />{v}{rel ? <em>{fmt(rel.count)}</em> : null}</span>; }) : <span className="t-mut" style={{ fontSize: ".82rem" }}>Feeds metadata; no typed verbs in this view.</span>}
            </div>
            {prod.kinds.length ? (
              <div className="row wrap" style={{ gap: 7, marginTop: 9 }}>
                <span className="t-mut" style={{ fontSize: ".72rem", textTransform: "uppercase", letterSpacing: ".06em" }}>Node kinds</span>
                {prod.kinds.map((kn) => <span key={kn} className="badge badge-neutral">{kn}</span>)}
              </div>
            ) : null}
          </div>

          {graphNode ? (
            <button className="btn-ghost active" style={{ justifyContent: "center" }} onClick={() => { onClose(); onOpenNode(graphNode, D.graph); }}>
              <Icon.graph size={14} /> See where it lands in the graph →
            </button>
          ) : (
            <button className="btn-ghost" style={{ justifyContent: "center" }} onClick={() => { onClose(); onGo && onGo("explorer"); }}>
              <Icon.graph size={14} /> Open the graph explorer →
            </button>
          )}

          <p className="t-mut" style={{ fontSize: ".72rem", borderTop: "1px solid var(--line)", paddingTop: 14, margin: 0, lineHeight: 1.5 }}>
            <span className="mono" style={{ color: "var(--subtle)" }}>{c.kind}</span> · commits facts to NornicDB on a <span className="mono">{c.cadence}</span> cadence. Truth & freshness from the envelope are preserved on every edge it writes.
          </p>
        </div>
      </aside>
    </>
  );
}

Object.assign(window, { resolveServiceId, tracePath, nodeNeighborhood, kindFacts, NodeDrawer, CollectorDrawer, COLLECTOR_PRODUCES, COLLECTOR_DOMAIN, collectorDomain, cloudResourceGraph, cloudResLabel });
