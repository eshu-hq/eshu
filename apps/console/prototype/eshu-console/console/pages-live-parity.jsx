/* Eshu Console - prototype pages for live-console parity surfaces.
   These pages derive their demo view from the same bundled graph facts used by
   the older dashboard, so the prototype keeps pace with live console routes. */
const { useMemo: useMemoP, useState: useStateP } = React;

function serviceImages(D) {
  if (Array.isArray(D.imageInventory) && D.imageInventory.length) {
    return D.imageInventory.map((image) => {
      const service = matchImageService(D, image);
      const vulnCount = service ? (D.vulns || []).filter((v) => (v.services || []).includes(service.id)).length : 0;
      return {
        service,
        image: image.image || image.name || image.digest,
        tag: image.tag || "—",
        vulnCount,
        truth: image.truth || "exact",
        sourceSystem: image.sourceSystem || image.registry || "registry"
      };
    });
  }
  return D.services.filter((s) => s.image).map((s) => {
    const vulnCount = (D.vulns || []).filter((v) => (v.services || []).includes(s.id)).length;
    const tag = String(s.image).split(":").pop() || "latest";
    return { service: s, image: s.image, tag, vulnCount, truth: s.truth, sourceSystem: "service catalog" };
  });
}

function iacRows(D) {
  if (Array.isArray(D.iacParityRows) && D.iacParityRows.length) return D.iacParityRows;
  const resources = (D.cloudResources || []).filter((r) => r.tf).map((r) => {
    const svc = r.service && D.servicesById[r.service];
    return {
      id: r.uid,
      name: r.name,
      kind: r.type,
      ownerId: svc ? svc.id : "",
      owner: svc ? svc.name : "shared platform",
      source: "Terraform state",
      account: r.account,
      region: r.region,
      truth: r.truth || "exact"
    };
  });
  const apps = (D.argocdApps || []).map((a) => ({
    id: "argocd:" + a.name,
    name: a.name,
    kind: "argocd_application",
    ownerId: a.indexed ? a.name : "",
    owner: a.indexed ? a.name : "deploy-only",
    source: "helm-charts/argocd",
    account: (a.env || []).join(", "),
    region: "kubernetes",
    truth: a.indexed ? "exact" : "derived"
  }));
  return resources.concat(apps);
}

function sbomRows(D) {
  if (D.sbomInventory && Array.isArray(D.sbomInventory.buckets) && D.sbomInventory.buckets.length) {
    return D.sbomInventory.buckets;
  }
  return (D.vulns || []).map((v) => {
    const services = (v.services || []).map((id) => D.servicesById[id]).filter(Boolean);
    return {
      id: v.cve,
      advisory: v.cve,
      pkg: v.pkg,
      version: v.version,
      ecosystem: v.ecosystem,
      severity: v.severity,
      source: v.source,
      fix: v.fixAvailable ? (v.fixed || "available") : "none",
      services
    };
  });
}

function dependencyRows(D) {
  if (Array.isArray(D.dependencyInventory) && D.dependencyInventory.length) return D.dependencyInventory;
  return D.services.flatMap((s) => {
    const deps = (s.deps || []).map((id) => ({
      id: s.id + "->" + id,
      source: s,
      target: D.servicesById[id] || { id, name: id, kind: "external", system: "external" },
      verb: "DEPENDS_ON",
      layer: "code"
    }));
    const stores = (s.stores || []).map((store) => ({
      id: s.id + "->store:" + store,
      source: s,
      target: { id: store, name: store, kind: "datastore", system: "cloud" },
      verb: "STORES_IN",
      layer: "infra"
    }));
    return deps.concat(stores);
  });
}

function matchImageService(D, image) {
  const imageText = String(image.image || image.name || image.repository || "");
  const tag = image.tag ? ":" + image.tag : "";
  return D.services.find((s) => s.image && (s.image.includes(imageText) || (tag && s.image.endsWith(tag)))) || null;
}

function platformTopologyGraph(D) {
  const nodes = [
    { id: "repo:source", kind: "repo", label: "api-node-platform", sub: "source repository", col: 0 },
    { id: "repo:helm", kind: "repo", label: "helm-charts", sub: "api-node chart values", col: 1 },
    { id: "repo:argocd", kind: "repo", label: "iac-eks-argocd", sub: "ArgoCD application", col: 2 },
    { id: "image:platform", kind: "image", label: "api-node-platform:10.3.2", sub: "ECR image", col: 2 },
    { id: "workload:platform", kind: "workload", label: "api-node-platform", sub: "Kubernetes Deployment :3081", col: 3, hero: true },
    { id: "cluster:bg-prod", kind: "aws", label: "eks-bg-prod", sub: "EKS cluster", col: 4 }
  ];
  const edges = [
    { s: "repo:helm", t: "repo:source", verb: "PACKAGES", layer: "deploy" },
    { s: "repo:argocd", t: "repo:helm", verb: "DEPLOYS_HELM", layer: "deploy" },
    { s: "image:platform", t: "repo:source", verb: "BUILT_FROM", layer: "deploy" },
    { s: "workload:platform", t: "repo:argocd", verb: "DEPLOYED_BY", layer: "deploy" },
    { s: "workload:platform", t: "image:platform", verb: "RUNS_IMAGE", layer: "runtime" },
    { s: "workload:platform", t: "cluster:bg-prod", verb: "RUNS_IN", layer: "runtime" }
  ];
  return { nodes, edges };
}

function Images({ data, onOpenService }) {
  const D = data || ESHU;
  const images = useMemoP(() => serviceImages(D), [D]);
  const vulnerable = images.filter((r) => r.vulnCount > 0).length;

  return (
    <div className="page">
      <div className="page-intro"><h2>Images</h2><p>Container images associated with indexed services, joined to package and vulnerability evidence.</p></div>
      <div className="grid g-4">
        <StatTile label="Images" value={images.length} color="var(--blue)" sub="service image refs" />
        <StatTile label="Vulnerable" value={vulnerable} color="var(--crit)" sub="images with advisories" />
        <StatTile label="Registries" value="1" color="var(--teal)" sub="ECR source" />
        <StatTile label="SBOM coverage" value={Math.max(0, images.length - vulnerable) + "/" + images.length} color="var(--med)" sub="package evidence present" />
      </div>
      <Panel className="flush mt" title="Image inventory" sub="Click a service image to open its service context" glyph={<Icon.box />}>
        <table className="tbl">
          <thead><tr><th>Image</th><th>Service</th><th>Tag</th><th>Advisories</th><th>Truth</th><th></th></tr></thead>
          <tbody>{images.map((row) => (
            <tr key={row.image} onClick={() => row.service && onOpenService(row.service.id)} style={{ cursor: row.service ? "pointer" : "default" }}>
              <td className="mono" style={{ fontSize: ".78rem" }}>{row.image}</td>
              <td className="t-name">{row.service ? row.service.name : "—"}</td>
              <td><Badge tone="teal">{row.tag}</Badge></td>
              <td><span className={row.vulnCount ? "nav-count alert" : "nav-count"}>{row.vulnCount}</span></td>
              <td><TruthChip level={row.truth} /></td>
              <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
            </tr>
          ))}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function IaC({ data, onOpenService }) {
  const D = data || ESHU;
  const rows = useMemoP(() => iacRows(D), [D]);
  const [q, setQ] = useStateP("");
  const filtered = rows.filter((r) => q === "" || (r.name + r.kind + r.owner + r.source).toLowerCase().includes(q.toLowerCase()));
  const terraform = rows.filter((r) => r.source === "Terraform state").length;
  const apps = rows.length - terraform;

  return (
    <div className="page">
      <div className="page-intro"><h2>IaC</h2><p>Terraform state and ArgoCD application evidence connected back to cloud resources and services.</p></div>
      <div className="grid g-4">
        <StatTile label="IaC objects" value={rows.length} color="#8b5cf6" sub="Terraform + ArgoCD" />
        <StatTile label="Terraform resources" value={terraform} color="var(--teal)" sub="declared cloud resources" />
        <StatTile label="ArgoCD apps" value={apps} color="var(--ember)" sub="helm chart deployments" />
        <StatTile label="Accounts" value={D.cloudAccounts.length} color="var(--blue)" sub="cloud scopes" />
      </div>
      <div className="repo-toolbar mt">
        <div className="searchbox" style={{ minWidth: 240, height: 38, margin: 0, flex: 1 }}><Icon.search size={16} /><input placeholder="Find IaC, app, resource or owner..." value={q} onChange={(e) => setQ(e.target.value)} /></div>
      </div>
      <Panel className="flush mt" title={filtered.length + " IaC records"} sub="Configuration source, cloud scope and owning service" glyph={<Icon.layers />}>
        <table className="tbl">
          <thead><tr><th>Name</th><th>Kind</th><th>Source</th><th>Owner</th><th>Scope</th><th>Truth</th></tr></thead>
          <tbody>{filtered.slice(0, 120).map((r) => {
            const svc = r.ownerId && D.servicesById[r.ownerId];
            return (
              <tr key={r.id} onClick={() => svc && onOpenService(svc.id)} style={{ cursor: svc ? "pointer" : "default" }}>
                <td className="t-name">{r.name}</td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{r.kind}</td>
                <td><Badge tone={r.source === "Terraform state" ? "teal" : "neutral"}>{r.source}</Badge></td>
                <td>{r.owner}</td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{r.account} · {r.region}</td>
                <td><TruthChip level={r.truth} /></td>
              </tr>
            );
          })}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function SBOM({ data, onOpenService }) {
  const D = data || ESHU;
  const rows = useMemoP(() => sbomRows(D), [D]);
  const packages = new Set(rows.map((r) => r.pkg)).size;
  const critical = rows.filter((r) => r.severity === "critical").length;
  const total = D.sbomSummary ? D.sbomSummary.total : rows.length;
  const liveBuckets = Boolean(D.sbomInventory);
  const fixCount = rows.filter((r) => r.fix && r.fix !== "none").length;

  return (
    <div className="page">
      <div className="page-intro"><h2>SBOM</h2><p>Package and advisory evidence correlated to deployed service images and source manifests.</p></div>
      <div className="grid g-4">
        <StatTile label={liveBuckets ? "Subjects" : "Packages"} value={packages} color="var(--teal)" sub={liveBuckets ? "grouped SBOM buckets" : "affected package names"} />
        <StatTile label="SBOM attachments" value={total} color="var(--crit)" sub="attestation evidence" />
        <StatTile label="Critical" value={critical} color="var(--crit)" sub="highest severity" />
        <StatTile label="Fix available" value={fixCount} color="var(--blue)" sub="upgrade candidates" />
      </div>
      <Panel className="flush mt" title="SBOM evidence" sub="Advisories joined to affected services" glyph={<Icon.shield />}>
        <table className="tbl">
          <thead><tr><th>Advisory</th><th>Package</th><th>Severity</th><th>Affected services</th><th>Fix</th><th>Source</th></tr></thead>
          <tbody>{rows.map((r) => (
            <tr key={r.id}>
              <td className="mono" style={{ fontSize: ".78rem" }}>{r.advisory}</td>
              <td><span className="cell-stack"><span className="t-name">{r.pkg}</span><small>{r.ecosystem} · {r.version}</small></span></td>
              <td>{r.severity ? <span className={"sev sev-" + r.severity}>{r.severity}</span> : <Badge tone="neutral">{r.kind || "bucket"}</Badge>}</td>
              <td><div className="row wrap" style={{ gap: 5 }}>{r.services.map((s) => <button key={s.id} className="dep-chip" onClick={() => onOpenService(s.id)}>{s.name}</button>)}</div></td>
              <td className="mono" style={{ fontSize: ".76rem" }}>{r.fix || (r.count ? fmt(r.count) + " attachment(s)" : "—")}</td>
              <td>{r.source}</td>
            </tr>
          ))}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function Dependencies({ data, onOpenService }) {
  const D = data || ESHU;
  const rows = useMemoP(() => dependencyRows(D), [D]);
  const [layer, setLayer] = useStateP("all");
  const filtered = rows.filter((r) => layer === "all" || r.layer === layer);
  const groups = ["all", "code", "infra"];

  return (
    <div className="page">
      <div className="page-intro"><h2>Dependencies</h2><p>Service, library and datastore dependencies inferred from package imports, catalog metadata and cloud resource joins.</p></div>
      <div className="grid g-4">
        <StatTile label="Edges" value={rows.length} color="var(--teal)" sub="code + infra" />
        <StatTile label="Code deps" value={rows.filter((r) => r.layer === "code").length} color="var(--blue)" sub="package imports" />
        <StatTile label="Datastores" value={rows.filter((r) => r.layer === "infra").length} color="var(--ember)" sub="storage dependencies" />
        <StatTile label="Services" value={D.services.filter((s) => s.kind !== "lib").length} color="var(--teal)" sub="running workloads" />
      </div>
      <div className="dep-toggle mt">{groups.map((g) => <button key={g} className={layer === g ? "active" : ""} onClick={() => setLayer(g)}>{g === "all" ? "All" : g}</button>)}</div>
      <Panel className="flush mt" title={filtered.length + " dependency edges"} sub="Click any endpoint to open the service drawer" glyph={<Icon.branch />}>
        <table className="tbl">
          <thead><tr><th>Source</th><th>Verb</th><th>Target</th><th>Layer</th><th>System</th></tr></thead>
          <tbody>{filtered.map((r) => (
            <tr key={r.id}>
              <td>{r.source ? <button className="dep-chip" onClick={() => onOpenService(r.source.id)}>{r.source.name}</button> : <span className="t-name">{r.sourceLabel}</span>}</td>
              <td><Badge tone={r.layer === "code" ? "teal" : "neutral"}>{r.verb}</Badge></td>
              <td>{r.target && D.servicesById[r.target.id] ? <button className="dep-chip" onClick={() => onOpenService(r.target.id)}>{r.target.name}</button> : <span className="t-name">{r.targetLabel || (r.target && r.target.name)}</span>}</td>
              <td className="mono" style={{ fontSize: ".76rem" }}>{r.layer}</td>
              <td>{r.system || (r.target && r.target.system)}</td>
            </tr>
          ))}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function Topology({ data, onOpenNode }) {
  const D = data || ESHU;
  const graph = useMemoP(() => platformTopologyGraph(D), [D]);
  const infra = (D.cloudResources || []).filter((r) => r.family !== "observability").length;

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro"><h2>Topology</h2><p>Code-to-cloud path for api-node-platform: source repository, Helm chart, ArgoCD repo, image, workload and cluster.</p></div>
      <div className="grid g-4">
        <StatTile label="Exposure" value="Internal" color="var(--teal)" sub="service mesh / VPC path" />
        <StatTile label="Deploy chain" value="4" color="var(--ember)" sub="source -> helm -> argocd -> workload" />
        <StatTile label="Infra in scope" value={infra} color="var(--blue)" sub="cloud resources" />
        <StatTile label="Runtime target" value="EKS" color="var(--teal)" sub="bg-prod cluster" />
      </div>
      <Panel className="flush mt" title="api-node-platform - deployment topology" sub="What deploys what, and where the resulting workload runs" glyph={<Icon.branch />}>
        <GraphCanvas graph={graph} layout="layered" height={620} onSelect={(n) => onOpenNode && onOpenNode(n, graph)} />
      </Panel>
    </div>
  );
}

Object.assign(window, { Images, IaC, SBOM, Dependencies, Topology, serviceImages, iacRows, sbomRows, dependencyRows, platformTopologyGraph });
