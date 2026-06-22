/* Eshu Console — graph drill-down: edge evidence model, unified inspector, breadcrumb.
   Exports to window. Loaded after graph.jsx, before pages. */
const { useMemo: useMemoX } = React;

/* typed-fact templates per relationship verb — what Eshu would attach to the edge */
const VERB_META = {
  IMPORTS: { desc: "Code-level package import resolved statically from the manifest.", facts: (s, t) => [`${s} IMPORTS ${t}`, "edge kind: code · static import", "resolved from package.json + lockfile"] },
  CALLS: { desc: "Symbol-level call edge between functions across the boundary.", facts: (s, t) => [`${s} CALLS ${t}`, "edge kind: code · call-graph", "resolved from AST + import binding"] },
  BUILDS: { desc: "Source repository builds a container image in CI.", facts: (s, t) => [`${s} BUILDS ${t}`, "ci: Pipelines / GitHub Actions", "registry: ECR (us-east-1)"] },
  PUBLISHES: { desc: "Repository publishes an internal npm package.", facts: (s, t) => [`${s} PUBLISHES ${t}`, "registry: npm · private @acme scope", "provenance: build attestation"] },
  DEPLOYS_FROM: { desc: "Workload is deployed from a built image via ArgoCD.", facts: (s, t) => [`${s} DEPLOYS_FROM ${t}`, "deployer: ArgoCD · Kustomize overlay", "namespace: api-node"] },
  DEPENDS_ON: { desc: "Runtime dependency observed between two services.", facts: (s, t) => [`${s} DEPENDS_ON ${t}`, "edge kind: runtime", "observed via call traffic + manifest"] },
  RUNS_AS: { desc: "Service runs as a Kubernetes workload object.", facts: (s, t) => [`${s} RUNS_AS ${t}`, "kind: Deployment", "orchestrator: EKS"] },
  RUNS_IN: { desc: "Workload is placed in an environment / namespace.", facts: (s, t) => [`${s} RUNS_IN ${t}`, "cluster: eks-<env>", "region: us-east-1 · ArgoCD-synced"] },
  STORES_IN: { desc: "Service reads from / writes to a datastore.", facts: (s, t) => [`${s} STORES_IN ${t}`, "access: read/write", "discovered from config + IRSA grants"] },
  ASSUMES_ROLE: { desc: "Workload assumes a scoped IRSA IAM role.", facts: (s, t) => [`${s} ASSUMES_ROLE ${t}`, "mechanism: IRSA (Crossplane XIRSARole)", "scope: least-privilege read"] },
  DECLARED_BY: { desc: "Cloud resource is declared by Terraform IaC.", facts: (s, t) => [`${s} DECLARED_BY ${t}`, "iac: Terraform state", "module: terraform-stack-* / iac-eks-*"] },
  AFFECTED_BY: { desc: "Component is affected by a known vulnerability.", facts: (s, t) => [`${s} AFFECTED_BY ${t}`, "join: SBOM ⋈ advisory feed", "feeds: KEV · EPSS · NVD · OSV · GHSA"] },
  OBSERVED_INCIDENT: { desc: "PagerDuty incident correlated to a service.", facts: (s, t) => [`${s} OBSERVED_INCIDENT ${t}`, "source: PagerDuty", "correlation: change-event window"] },
  TRACKED_BY: { desc: "Jira work item linked to a service or change.", facts: (s, t) => [`${s} TRACKED_BY ${t}`, "source: Jira · ACME-NODE", "link: commit / deploy reference"] },
  DISCOVERS_CONFIG_IN: { desc: "ArgoCD/Kustomize overlay discovered for a workload.", facts: (s, t) => [`${s} DISCOVERS_CONFIG_IN ${t}`, "path: helm-charts/<app>", "overlay: kustomize"] },
  EMITS_METRICS: { desc: "Workload ships metrics to Prometheus / Grafana Mimir.", facts: (s, t) => [`${s} EMITS_METRICS ${t}`, "collector: prometheus_mimir", "RED + USE panels · alert rules"] },
  TRACED_BY: { desc: "Service emits OpenTelemetry spans to the collector.", facts: (s, t) => [`${s} TRACED_BY ${t}`, "collector: otel_traces (tail-sampled)", "spans derive DEPENDS_ON from real traffic"] },
  LOGS_TO: { desc: "Workload streams structured logs to Grafana Loki.", facts: (s, t) => [`${s} LOGS_TO ${t}`, "collector: grafana_loki", "trace ⋈ log correlation by trace-id"] },
  FRONTED_BY: { desc: "Portal traffic fronted by a Cloudflare edge route + WAF.", facts: (s, t) => [`${s} FRONTED_BY ${t}`, "collector: cloudflare", "WAF events + cache rules + edge routes"] },
  SECURED_BY: { desc: "Workload's network exposure is gated by a security group.", facts: (s, t) => [`${s} SECURED_BY ${t}`, "edge kind: networking", "ingress/egress rules · port 3081"] },
  ROUTES_TO: { desc: "Egress routed through a NAT / internet gateway.", facts: (s, t) => [`${s} ROUTES_TO ${t}`, "edge kind: networking", "route table · 0.0.0.0/0 → NAT"] },
  ATTACHED_TO: { desc: "Network object attached to a VPC / subnet.", facts: (s, t) => [`${s} ATTACHED_TO ${t}`, "edge kind: networking", "discovered from VPC topology"] }
};

function edgeFactText(value) {
  if (typeof value === "string") return value;
  if (!value || typeof value !== "object") return String(value);
  const parts = [
    value.source,
    value.kind || value.type || value.relationship_type || value.verb,
    value.location || value.path || value.file,
    value.detail || value.summary
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : JSON.stringify(value);
}

function edgeProvidedFacts(edge) {
  const facts = [];
  [edge.facts, edge.evidence].forEach((items) => {
    if (Array.isArray(items)) items.forEach((item) => facts.push(edgeFactText(item)));
  });
  return facts.filter(Boolean);
}

function entityMapEdgeEvidence(rel, verb, incoming) {
  const labels = Array.isArray(rel.entity_labels) ? rel.entity_labels.filter(Boolean).join(", ") : "";
  return [
    "relationship source: " + String(rel.relationship_source || "graph"),
    "direction: " + (incoming ? "incoming" : "outgoing"),
    labels ? "entity labels: " + labels : "",
    rel.repo_id ? "repo: " + rel.repo_id : "",
    rel.environment ? "environment: " + rel.environment : "",
    rel.depth != null ? "depth: " + rel.depth : ""
  ].filter(Boolean);
}

/* resolve human-readable evidence for an edge within a graph */
function edgeEvidence(edge, graph, data) {
  const D = data || ESHU;
  const sNode = graph.nodes.find((n) => n.id === edge.s);
  const tNode = graph.nodes.find((n) => n.id === edge.t);
  const sLabel = (sNode && sNode.label) || edge.s;
  const tLabel = (tNode && tNode.label) || edge.t;
  const meta = VERB_META[edge.verb] || { desc: "Typed relationship in the NornicDB graph.", facts: (s, t) => [`${s} ${edge.verb} ${t}`] };
  const rel = (D.relationships || []).find((r) => r.verb === edge.verb);
  const provided = edgeProvidedFacts(edge);
  if (D.org === "live") {
    return {
      sLabel, tLabel, sNode, tNode,
      desc: edge.detail || edge.summary || "Live graph relationship returned by the active query.",
      facts: provided.length ? provided : [`${sLabel} ${edge.verb} ${tLabel}`, "relationship source metadata unavailable"],
      count: edge.count != null ? edge.count : null,
      layer: edge.layer
    };
  }
  return {
    sLabel, tLabel, sNode, tNode,
    desc: (rel && rel.detail) || meta.desc,
    facts: provided.length ? provided : meta.facts(sLabel, tLabel),
    count: edge.count != null ? edge.count : rel ? rel.count : null,
    layer: edge.layer
  };
}

/* list of {edge, dir, neighborId, neighborLabel} adjacent to a node */
function nodeConnections(node, graph) {
  const out = [];
  graph.edges.forEach((e) => {
    if (e.s === node.id) {
      const nb = graph.nodes.find((n) => n.id === e.t);
      out.push({ edge: e, dir: "out", neighborId: e.t, neighborLabel: (nb && nb.label) || e.t, neighborKind: nb && nb.kind });
    } else if (e.t === node.id) {
      const nb = graph.nodes.find((n) => n.id === e.s);
      out.push({ edge: e, dir: "in", neighborId: e.s, neighborLabel: (nb && nb.label) || e.s, neighborKind: nb && nb.kind });
    }
  });
  return out;
}

function Breadcrumb({ trail, onCrumb }) {
  if (!trail || !trail.length) return null;
  return (
    <nav className="drill-crumbs" aria-label="Drill path">
      {trail.map((c, i) => (
        <React.Fragment key={i}>
          {i > 0 ? <span className="crumb-sep">›</span> : null}
          <button type="button" className={cx("crumb", i === trail.length - 1 && "is-current")} onClick={() => onCrumb && onCrumb(i)} disabled={i === trail.length - 1}>
            {c.kind ? <i className="crumb-dot" style={{ background: (ESHU.kindStyle[c.kind] || {}).color || "var(--accent)" }} /> : null}{c.label}
          </button>
        </React.Fragment>
      ))}
    </nav>
  );
}

/* Edge detail — verb, endpoints, typed facts, actions */
function EdgeInspector({ edge, graph, data, onOpenService, onSelectNode, onIsolate, isolated, onPin, pinned }) {
  const D = data || ESHU;
  const info = edgeEvidence(edge, graph, D);
  const col = ESHU.layerColor[edge.layer] || "var(--teal)";
  const svc = (id) => D.servicesById && D.servicesById[id];
  return (
    <div className="inspector edge-insp" style={{ "--ec": col }}>
      <div className="insp-head">
        <span className="edge-glyph" style={{ color: col, borderColor: col }}>↔</span>
        <div>
          <div className="insp-kind">Relationship · {edge.layer}</div>
          <div className="insp-title" style={{ color: col, fontSize: "1rem" }}>{edge.verb}</div>
        </div>
      </div>
      <div className="edge-flow">
        <button className="edge-node-btn" onClick={() => onSelectNode(info.sNode)}><i style={{ background: (ESHU.kindStyle[info.sNode && info.sNode.kind] || {}).color }} />{info.sLabel}</button>
        <span className="edge-flow-verb" style={{ color: col }}>{edge.verb} →</span>
        <button className="edge-node-btn" onClick={() => onSelectNode(info.tNode)}><i style={{ background: (ESHU.kindStyle[info.tNode && info.tNode.kind] || {}).color }} />{info.tLabel}</button>
      </div>
      <p style={{ color: "var(--muted)", lineHeight: 1.55, margin: 0, fontSize: ".85rem" }}>{info.desc}</p>
      <div>
        <div className="section-label">Typed facts on this edge</div>
        <div className="insp-evi">{info.facts.map((f, i) => <div className="insp-evi-row" key={i}>{f}</div>)}</div>
      </div>
      <div className="row wrap" style={{ gap: 8 }}>
        <span className="edge-layer-chip" style={{ "--ec": col }}><i />{edge.layer} layer</span>
        {info.count != null ? <span className="badge">{fmt(info.count)} like this in graph</span> : null}
      </div>
      <div className="insp-actions">
        {svc(edge.s) ? <button className="btn-ghost" onClick={() => onOpenService(edge.s)}>Open {info.sLabel} →</button> : null}
        {svc(edge.t) ? <button className="btn-ghost" onClick={() => onOpenService(edge.t)}>Open {info.tLabel} →</button> : null}
        {onPin ? <button className={cx("btn-ghost", pinned && "active")} onClick={() => onPin(edge)}>{pinned ? "◉ Unpin edge" : "◈ Pin this edge"}</button> : null}
        {onIsolate ? <button className={cx("btn-ghost", isolated && "active")} onClick={() => onIsolate(edge)}>{isolated ? "Clear isolation" : "Isolate " + edge.verb + " edges"}</button> : null}
      </div>
    </div>
  );
}

/* Node detail — reuses NodeInspector evidence, adds clickable connections + drill actions */
function NodeDrillInspector({ node, graph, data, onOpenService, onOpenNode, onSelectNode, onSelectEdge, onExpand, onFocus, expandedIds }) {
  const conns = useMemoX(() => nodeConnections(node, graph), [node, graph]);
  const canExpand = onExpand && (!expandedIds || !expandedIds.has(node.id));
  return (
    <div className="inspector">
      <NodeInspector node={node} data={data} onOpenService={onOpenService} />
      {(onExpand || onFocus || onOpenNode) ? (
        <div className="insp-actions">
          {onOpenNode ? <button className="btn-ghost active" onClick={() => onOpenNode(node, graph)}><Icon.external size={13} /> Open node detail</button> : null}
          {canExpand ? <button className="btn-ghost" onClick={() => onExpand(node)}>＋ Expand neighbours</button> : null}
          {onFocus ? <button className="btn-ghost" onClick={() => onFocus(node)}>◎ Focus here</button> : null}
        </div>
      ) : null}
      <div>
        <div className="section-label">Connections · {conns.length} {conns.length === 1 ? "edge" : "edges"}</div>
        {conns.length ? (
          <div className="conn-list">
            {conns.map((c, i) => {
              const col = ESHU.layerColor[c.edge.layer] || "var(--teal)";
              return (
                <div className="conn-row" key={i} style={{ "--ec": col }}>
                  <button type="button" className="conn-main" onClick={() => onSelectEdge(c.edge)} title="Inspect this relationship">
                    <span className="conn-dir" style={{ color: col }}>{c.dir === "out" ? "→" : "←"}</span>
                    <span className="conn-verb" style={{ color: col }}>{c.edge.verb}</span>
                  </button>
                  <button type="button" className="conn-neighbor" onClick={() => onSelectNode(c.neighborId)} title="Jump to this node">
                    <i style={{ background: (ESHU.kindStyle[c.neighborKind] || {}).color || "var(--subtle)" }} />{c.neighborLabel}
                  </button>
                </div>
              );
            })}
          </div>
        ) : <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>No edges from this node in the current view.</p>}
      </div>
    </div>
  );
}

/* Unified inspector: renders node OR edge selection, with optional breadcrumb */
function GraphInspector({ sel, graph, data, onOpenService, onOpenNode, onSelectNode, onSelectEdge, onExpand, onFocus, onIsolate, onPin, pinnedEdge, isolatedVerb, breadcrumb, onCrumb, expandedIds, emptyHint }) {
  if (!sel) return <p className="empty">{emptyHint || "Select a node or relationship to inspect its evidence."}</p>;
  const resolveNode = (idOrNode) => typeof idOrNode === "string" ? graph.nodes.find((n) => n.id === idOrNode) : idOrNode;
  return (
    <>
      {breadcrumb && breadcrumb.length > 1 ? <Breadcrumb trail={breadcrumb} onCrumb={onCrumb} /> : null}
      {sel.type === "edge" ? (
        <EdgeInspector edge={sel.edge} graph={graph} data={data} onOpenService={onOpenService} onSelectNode={(n) => onSelectNode(resolveNode(n))} onIsolate={onIsolate} isolated={isolatedVerb === sel.edge.verb} onPin={onPin} pinned={pinnedEdge && edgeKey(pinnedEdge) === edgeKey(sel.edge)} />
      ) : (
        <NodeDrillInspector node={sel.node} graph={graph} data={data} onOpenService={onOpenService} onOpenNode={onOpenNode} onSelectNode={(n) => onSelectNode(resolveNode(n))} onSelectEdge={onSelectEdge} onExpand={onExpand} onFocus={onFocus} expandedIds={expandedIds} />
      )}
    </>
  );
}

Object.assign(window, { VERB_META, edgeEvidence, entityMapEdgeEvidence, nodeConnections, Breadcrumb, EdgeInspector, NodeDrillInspector, GraphInspector });
