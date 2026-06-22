/* Eshu Console - prototype pages for live-console parity surfaces.
   These pages derive their demo view from the same bundled graph facts used by
   the older dashboard, so the prototype keeps pace with live console routes. */
const { useEffect: useEffectP, useMemo: useMemoP, useState: useStateP } = React;

function serviceImages(D) {
  if (Array.isArray(D.imageInventory) && D.imageInventory.length) {
    return D.imageInventory.map((image) => ({
      id: image.id || image.digest || image.image,
      digest: image.digest || "",
      registry: image.registry || "",
      repository: image.repository || image.name || image.image || "",
      tag: image.tag || "",
      mediaType: image.mediaType || image.artifactType || "",
      sizeBytes: typeof image.sizeBytes === "number" ? image.sizeBytes : null,
      truth: image.truth || "exact",
      sourceSystem: image.sourceSystem || image.registry || "registry"
    }));
  }
  return D.services.filter((s) => s.image).map((s) => {
    const tag = String(s.image).split(":").pop() || "latest";
    return { id: s.image, digest: "", registry: "", repository: s.image, tag, mediaType: "", sizeBytes: null, truth: s.truth, sourceSystem: "service catalog" };
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
    return {
      id: v.cve,
      value: v.cve,
      dimension: "demo_advisory_subject",
      count: (v.services || []).length || 1
    };
  });
}

function dependencyRows(D) {
  if (Array.isArray(D.dependencyInventory) && D.dependencyInventory.length) return D.dependencyInventory;
  return D.services.flatMap((s) => {
    return (s.deps || []).map((id) => {
      const target = D.servicesById[id] || { id, name: id };
      return {
        id: s.id + "->" + id,
        direction: "forward",
        anchorPackage: s.name,
        anchorPackageId: s.repo || s.id,
        declaringVersion: s.version || "",
        relatedPackage: target.name,
        relatedPackageId: target.id,
        ecosystem: "demo",
        range: "",
        dependencyType: "runtime",
        optional: false
      };
    });
  });
}

function platformTopologyGraph(D) {
  const nodes = [
    { id: "repo:source", kind: "repo", label: "svc-platform", sub: "source repository", col: 0 },
    { id: "repo:helm", kind: "repo", label: "helm-charts", sub: "api-node chart values", col: 1 },
    { id: "repo:argocd", kind: "repo", label: "iac-eks-argocd", sub: "ArgoCD application", col: 2 },
    { id: "image:platform", kind: "image", label: "svc-platform:10.3.2", sub: "ECR image", col: 2 },
    { id: "workload:platform", kind: "workload", label: "svc-platform", sub: "Kubernetes Deployment :3081", col: 3, hero: true },
    { id: "cluster:acme-prod", kind: "aws", label: "eks-acme-prod", sub: "EKS cluster", col: 4 }
  ];
  const edges = [
    { s: "repo:helm", t: "repo:source", verb: "PACKAGES", layer: "deploy" },
    { s: "repo:argocd", t: "repo:helm", verb: "DEPLOYS_HELM", layer: "deploy" },
    { s: "image:platform", t: "repo:source", verb: "BUILT_FROM", layer: "deploy" },
    { s: "workload:platform", t: "repo:argocd", verb: "DEPLOYED_BY", layer: "deploy" },
    { s: "workload:platform", t: "image:platform", verb: "RUNS_IMAGE", layer: "runtime" },
    { s: "workload:platform", t: "cluster:acme-prod", verb: "RUNS_IN", layer: "runtime" }
  ];
  return { nodes, edges };
}

function topologyServices(D) {
  return (D.services || []).filter((s) => s.kind !== "lib");
}

function unwrapEnvelope(response) {
  if (response && response.error) throw new Error(response.error.message || response.error.code || "api error");
  return response && response.data && response.error !== undefined ? response.data : response;
}

async function optionalTopologyGet(client, path) {
  try { return unwrapEnvelope(await client.get(path)); }
  catch (_) { return null; }
}

function topologyNonEmpty() {
  for (let i = 0; i < arguments.length; i += 1) {
    const value = arguments[i];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function topologyLaneList(context) {
  return (context && Array.isArray(context.deployment_lanes) ? context.deployment_lanes : []).map((lane) => ({
    label: topologyNonEmpty(lane.label, lane.lane_type, "deployment lane"),
    environments: Array.isArray(lane.environments) ? lane.environments : [],
    sourceRepos: Array.isArray(lane.source_repositories) ? lane.source_repositories : []
  }));
}

function firstTopologyLaneValue(lanes, key) {
  for (const lane of lanes) {
    const values = lane[key] || [];
    if (values.length) return values[0];
  }
  return "";
}

function topologyTrafficPath(context, service, lanes) {
  const runtime = topologyNonEmpty(firstTopologyLaneValue(lanes, "environments"), (service.envs || [])[0], "runtime pending");
  const sourceRepo = topologyNonEmpty(firstTopologyLaneValue(lanes, "sourceRepos"), context && context.repo_name, service.repo, "source repo pending");
  const network = context && Array.isArray(context.network_paths) ? context.network_paths[0] : null;
  if (network) {
    return {
      edge: topologyNonEmpty(network.from_type, "entrypoint").replace(/_/g, " "),
      environment: topologyNonEmpty(network.environment, runtime),
      evidenceKind: topologyNonEmpty(network.path_type, "network_path"),
      hostname: topologyNonEmpty(network.from, service.name),
      origin: topologyNonEmpty(network.to_type, "runtime target").replace(/_/g, " "),
      reason: topologyNonEmpty(network.reason, "network path evidence"),
      runtime: topologyNonEmpty(network.to, runtime),
      sourceRepo,
      visibility: topologyNonEmpty(network.visibility, "visibility pending"),
      workload: topologyNonEmpty(network.workload, service.name)
    };
  }
  const edgeRuntime = (context && context.edge_runtime_evidence) || {};
  const apiDomain = (edgeRuntime.api_gateway_domains || edgeRuntime.apigateway_domains || [])[0];
  if (apiDomain) {
    const mapping = (apiDomain.api_mappings || [])[0] || {};
    return {
      edge: "API Gateway " + topologyNonEmpty(apiDomain.api_kind, "domain"),
      environment: topologyNonEmpty(mapping.stage, runtime),
      evidenceKind: "aws_apigateway_domain_name",
      hostname: topologyNonEmpty(apiDomain.domain_name, service.name),
      origin: topologyNonEmpty(apiDomain.regional_domain_name, apiDomain.distribution_domain_name, "origin pending"),
      reason: mapping.api_id ? "custom domain maps to API " + mapping.api_id : "API Gateway custom domain evidence",
      runtime,
      sourceRepo,
      visibility: "public",
      workload: service.name
    };
  }
  const dist = (edgeRuntime.cloudfront_distributions || edgeRuntime.distributions || [])[0];
  if (!dist) return null;
  const origin = (dist.origins || [])[0] || {};
  return {
    edge: topologyNonEmpty(dist.id, dist.domain_name, "CloudFront distribution"),
    environment: runtime,
    evidenceKind: "aws_cloudfront_distribution",
    hostname: topologyNonEmpty((dist.aliases || [])[0], dist.domain_name, service.name),
    origin: topologyNonEmpty(origin.id, origin.domain_name, "origin pending"),
    reason: "CloudFront distribution " + topologyNonEmpty(dist.id, dist.domain_name),
    runtime,
    sourceRepo,
    visibility: "public",
    workload: service.name
  };
}

function liveTopologyGraph(service, story, context) {
  const merged = Object.assign({}, story || {}, context || {});
  if (story && story.service_identity) {
    merged.name = topologyNonEmpty(story.service_identity.service_name, story.service_name, merged.name);
    merged.repo_name = topologyNonEmpty(merged.repo_name, story.service_identity.repo_name);
  }
  const lanes = topologyLaneList(merged);
  const path = topologyTrafficPath(merged, service, lanes);
  const repoName = topologyNonEmpty(path && path.sourceRepo, merged.repo_name, service.repo, "source repo pending");
  const serviceName = topologyNonEmpty(path && path.workload, merged.name, service.name);
  const runtime = topologyNonEmpty(path && path.runtime, firstTopologyLaneValue(lanes, "environments"), (service.envs || [])[0], "runtime pending");
  const deployChain = window.liveDeploymentChainGraph && window.liveDeploymentChainGraph((merged.deployment_evidence || {}).artifacts, repoName, serviceName);
  const nodes = [];
  const edges = [];
  function add(node) { nodes.push(node); }
  function edge(s, t, verb, layer) { edges.push({ s, t, verb, layer }); }
  if (path) {
    add({ id: "hostname", kind: "service", label: path.hostname, sub: path.visibility + " · " + path.evidenceKind, col: 0 });
    add({ id: "edge", kind: "aws", label: path.edge, sub: "edge runtime evidence", col: 1 });
    add({ id: "origin", kind: "aws", label: path.origin, sub: path.reason, col: 2 });
    edge("hostname", "edge", "ROUTES_TO", "infra");
    edge("edge", "origin", "ORIGINATES_AT", "infra");
  } else {
    add({ id: "entry-pending", kind: "workitem", label: "Entry evidence pending", sub: "traffic evidence unavailable", col: 0 });
  }
  add({ id: "runtime", kind: "workload", label: runtime, sub: topologyNonEmpty(path && path.environment, runtime), col: 3 });
  add({ id: "workload", kind: "workload", label: serviceName, sub: service.kind + " · " + service.truth, col: 4, hero: true });
  edge(path ? "origin" : "entry-pending", "runtime", "RUNS_ON", "runtime");
  edge("runtime", "workload", "HOSTS", "runtime");
  if (deployChain) {
    deployChain.nodes.forEach(add);
    deployChain.edges.forEach((item) => edge(item.s, item.t, item.verb, item.layer));
  } else {
    add({ id: "repo", kind: "repo", label: repoName, sub: "source repository", col: 1 });
    add({ id: "delivery", kind: "service", label: "Delivery evidence", sub: lanes.length ? lanes.map((l) => l.label).slice(0, 2).join(" + ") : "deployment lane pending", col: 2 });
    edge("repo", "delivery", "BUILDS", "deploy");
    edge("delivery", "workload", "DEPLOYS", "deploy");
  }
  return {
    graph: { nodes, edges },
    meta: {
      deployChain: path ? "4" : "0",
      environment: topologyNonEmpty(path && path.environment, runtime),
      exposure: path ? path.visibility : "traffic evidence unavailable",
      serviceName
    }
  };
}

function Images({ data, onOpenService }) {
  const D = data || ESHU;
  const images = useMemoP(() => serviceImages(D), [D]);
  const registries = new Set(images.map((r) => r.registry).filter(Boolean)).size;
  const repositories = new Set(images.map((r) => r.repository).filter(Boolean)).size;
  const tagged = images.filter((r) => r.tag).length;

  return (
    <div className="page">
      <div className="page-intro"><h2>Images</h2><p>Container images from <span className="mono">GET /api/v0/images</span>: digest, tags, registry/repository, media type, and size.</p></div>
      <div className="grid g-4">
        <StatTile label="Images" value={images.length} color="var(--blue)" sub="bounded page from OCI inventory" />
        <StatTile label="Registries" value={registries} color="var(--teal)" sub="distinct in this page" />
        <StatTile label="Repositories" value={repositories} color="var(--violet)" sub="image repositories" />
        <StatTile label="Tagged" value={tagged} color="var(--ember)" sub="rows with tags" />
      </div>
      <Panel className="flush mt" title="Image inventory" sub="image node properties only" glyph={<Icon.box />}>
        <table className="tbl">
          <thead><tr><th>Repository</th><th>Tag</th><th>Digest</th><th>Media type</th><th>Size</th><th>Truth</th></tr></thead>
          <tbody>{images.map((row) => (
            <tr key={row.id || row.repository || row.digest}>
              <td className="t-name">{row.repository || "—"}{row.registry ? <div className="t-mut mono" style={{ fontSize: ".72rem" }}>{row.registry}</div> : null}</td>
              <td>{row.tag ? <Badge tone="teal">{row.tag}</Badge> : <span className="t-mut">—</span>}</td>
              <td className="mono" style={{ fontSize: ".74rem" }}>{row.digest ? (row.digest.length > 19 ? row.digest.slice(0, 19) + "…" : row.digest) : "—"}</td>
              <td className="mono" style={{ fontSize: ".72rem" }}>{row.mediaType || "—"}</td>
              <td>{row.sizeBytes === null ? "—" : fmt(row.sizeBytes)}</td>
              <td><TruthChip level={row.truth} /></td>
            </tr>
          ))}{images.length === 0 ? <tr><td colSpan={6} className="empty">No container images from this source.</td></tr> : null}</tbody>
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
      <div className="page-intro"><h2>IaC</h2><p>Terraform state and ArgoCD application evidence from <span className="mono">GET /api/v0/iac/resources</span>.</p></div>
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
            const source = r.relativePath || r.source;
            const line = r.lineNumber ? ":" + r.lineNumber : "";
            return (
              <tr key={r.id} onClick={() => svc && onOpenService(svc.id)} style={{ cursor: svc ? "pointer" : "default" }}>
                <td className="t-name">{r.name}<div className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.resourceName || r.category || "resource"}</div></td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{r.kind}</td>
                <td><Badge tone={source === "Terraform state" ? "teal" : "neutral"}>{source}{line}</Badge></td>
                <td>{r.owner}</td>
                <td className="mono" style={{ fontSize: ".76rem" }}>{r.account} · {r.region}</td>
                <td><TruthChip level={r.truth} /></td>
              </tr>
            );
          })}{filtered.length === 0 ? <tr><td colSpan={6} className="empty">No Terraform/IaC resources have been indexed yet.</td></tr> : null}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function SBOM({ data }) {
  const D = data || ESHU;
  const rows = useMemoP(() => sbomRows(D), [D]);
  const total = D.sbomSummary ? D.sbomSummary.total : rows.reduce((sum, r) => sum + (r.count || 0), 0);
  const verified = D.sbomSummary && D.sbomSummary.byStatus ? (D.sbomSummary.byStatus.attached_verified || 0) : 0;
  const sbomCount = D.sbomSummary && D.sbomSummary.byArtifactKind ? (D.sbomSummary.byArtifactKind.sbom || 0) : 0;
  const attestCount = D.sbomSummary && D.sbomSummary.byArtifactKind ? (D.sbomSummary.byArtifactKind.attestation || 0) : 0;
  const groupBy = (D.sbomInventory && D.sbomInventory.groupBy) || "subject_digest";

  return (
    <div className="page">
      <div className="page-intro"><h2>SBOM &amp; Attestations</h2><p>Supply-chain attestation evidence from <span className="mono">GET /api/v0/supply-chain/sbom-attestations/attachments</span>: subject digest inventory and provenance drilldown.</p></div>
      <div className="grid g-4">
        <StatTile label="Attachments" value={total} color="var(--teal)" sub="subject attachments" />
        <StatTile label="Verified" value={D.sbomSummary ? verified + "/" + (total || 0) : "—"} color="var(--blue)" sub="attached_verified" />
        <StatTile label="SBOM docs" value={D.sbomSummary ? sbomCount : "—"} color="var(--violet)" sub="artifact_kind=sbom" />
        <StatTile label="Attestations" value={D.sbomSummary ? attestCount : "—"} color="var(--ember)" sub="artifact_kind=attestation" />
      </div>
      <Panel className="flush mt" title={rows.length + " subjects"} sub={groupBy + " inventory"} glyph={<Icon.shield />}>
        <table className="tbl">
          <thead><tr><th>Subject digest</th><th>Attachments</th><th>Group</th><th>Source</th></tr></thead>
          <tbody>{rows.map((r) => {
            const value = r.value || r.id || "subject";
            const short = value.length > 28 ? value.slice(0, 21) + "…" + value.slice(-6) : value;
            return (
            <tr key={r.id || value}>
              <td className="mono" style={{ fontSize: ".78rem" }} title={value}>{short}</td>
              <td><Badge tone="teal">{r.count || 0}</Badge></td>
              <td className="mono" style={{ fontSize: ".76rem" }}>{r.dimension || groupBy}</td>
              <td>sbom-attestations</td>
            </tr>
          );})}{rows.length === 0 ? <tr><td colSpan={4} className="empty">No SBOM/attestation subjects from this source.</td></tr> : null}</tbody>
        </table>
      </Panel>
      <Panel className="mt" title="Attestation provenance" sub="per-subject attachment drilldown" glyph={<Icon.shield />}>
        <p className="t-mut">Select a subject in the live console to read <span className="mono">?subject_digest=...</span> attachments, repositories, workloads, services, components, and missing evidence from the same API family.</p>
      </Panel>
    </div>
  );
}

function Dependencies({ data, onOpenService }) {
  const D = data || ESHU;
  const rows = useMemoP(() => dependencyRows(D), [D]);
  const optional = rows.filter((r) => r.optional).length;
  const ecosystems = new Set(rows.map((r) => r.ecosystem).filter(Boolean)).size;
  const forward = rows.filter((r) => r.direction !== "reverse").length;

  return (
    <div className="page">
      <div className="page-intro"><h2>Dependencies</h2><p>Package dependency inventory from <span className="mono">GET /api/v0/dependencies</span>: forward rows answer what an anchor package depends on; reverse rows answer who depends on it.</p></div>
      <div className="grid g-4">
        <StatTile label="Edges" value={rows.length} color="var(--teal)" sub="bounded package graph page" />
        <StatTile label="Forward" value={forward} color="var(--blue)" sub="depends-on rows" />
        <StatTile label="Ecosystems" value={ecosystems} color="var(--violet)" sub="package ecosystems" />
        <StatTile label="Optional" value={optional} color="var(--ember)" sub="optional edges" />
      </div>
      <Panel className="flush mt" title={rows.length + " package dependency rows"} sub="package-native graph edges" glyph={<Icon.branch />}>
        <table className="tbl">
          <thead><tr><th>Anchor package</th><th>Version</th><th>Depends on</th><th>Ecosystem</th><th>Range</th><th>Type</th><th>Optional</th></tr></thead>
          <tbody>{rows.map((r) => (
            <tr key={r.id}>
              <td className="t-name">{r.anchorPackage || "—"}</td>
              <td className="mono" style={{ fontSize: ".76rem" }}>{r.declaringVersion || "—"}</td>
              <td className="t-name mono" style={{ fontSize: ".82rem" }} title={r.relatedPackageId}>{r.relatedPackage || "—"}</td>
              <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.ecosystem || "—"}</td>
              <td className="mono" style={{ fontSize: ".76rem" }}>{r.range || "—"}</td>
              <td>{r.dependencyType ? <Badge tone="teal">{r.dependencyType}</Badge> : <span className="t-mut">—</span>}</td>
              <td>{r.optional ? <Badge tone="warn">optional</Badge> : <span className="t-mut">no</span>}</td>
            </tr>
          ))}{rows.length === 0 ? <tr><td colSpan={7} className="empty">No package dependencies in the indexed package graph yet.</td></tr> : null}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function Topology({ data, client, onOpenNode, onOpenService }) {
  const D = data || ESHU;
  const services = useMemoP(() => topologyServices(D), [D]);
  const [selected, setSelected] = useStateP(() => (services[0] && services[0].id) || "");
  const [live, setLive] = useStateP(null);
  const [status, setStatus] = useStateP("idle");
  const service = services.find((s) => s.id === selected) || services[0] || { id: "service", name: "service pending", kind: "service", truth: "unavailable", envs: [] };
  useEffectP(() => {
    if (!services.some((s) => s.id === selected) && services[0]) setSelected(services[0].id);
  }, [services, selected]);
  useEffectP(() => {
    let cancelled = false;
    async function loadTopology() {
      if (!client || !service || !service.name) { setLive(null); setStatus("idle"); return; }
      setStatus("loading");
      setLive(null);
      const enc = encodeURIComponent(service.name);
      const story = await optionalTopologyGet(client, "/api/v0/services/" + enc + "/story");
      const context = await optionalTopologyGet(client, "/api/v0/services/" + enc + "/context");
      if (cancelled) return;
      setLive(liveTopologyGraph(service, story, context));
      setStatus(story || context ? "live" : "unavailable");
    }
    loadTopology();
    return () => { cancelled = true; };
  }, [client, service.id]);
  const demoGraph = useMemoP(() => platformTopologyGraph(D), [D]);
  const graph = client && live ? live.graph : demoGraph;
  const meta = client && live ? live.meta : { deployChain: "4", environment: "acme-prod cluster", exposure: "Internal", serviceName: "svc-platform" };
  const infra = (D.cloudResources || []).filter((r) => r.family !== "observability").length;

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro"><h2>Topology</h2><p>Code-to-cloud path for {meta.serviceName}: source repository, delivery evidence, runtime target and ingress evidence.</p></div>
      <div className="repo-toolbar">
        <select className="code-repo-select mono" aria-label="Service" value={service.id} onChange={(e) => setSelected(e.target.value)}>
          {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <Badge tone={status === "live" ? "teal" : "neutral"}>{client ? (status === "live" ? "Live topology" : status === "loading" ? "Loading" : "traffic evidence unavailable") : "Demo topology"}</Badge>
        {onOpenService ? <button className="btn-ghost" onClick={() => onOpenService(service.id)}>Open service</button> : null}
      </div>
      <div className="grid g-4">
        <StatTile label="Exposure" value={meta.exposure} color="var(--teal)" sub={client ? "from service story/context" : "service mesh / VPC path"} />
        <StatTile label="Deploy chain" value={meta.deployChain} color="var(--ember)" sub="repo -> delivery -> workload" />
        <StatTile label="Infra in scope" value={infra} color="var(--blue)" sub="cloud resources" />
        <StatTile label="Runtime target" value={meta.environment || "pending"} color="var(--teal)" sub={client ? "/api/v0/services/{name}/context" : "acme-prod cluster"} />
      </div>
      <Panel className="flush mt" title={meta.serviceName + " - deployment topology"} sub="What deploys what, and where the resulting workload runs" glyph={<Icon.branch />}>
        <GraphCanvas graph={graph} data={D} layout="layered" height={620} onSelect={(n) => onOpenNode && onOpenNode(n, graph)} />
      </Panel>
    </div>
  );
}

Object.assign(window, { Images, IaC, SBOM, Dependencies, Topology, serviceImages, iacRows, sbomRows, dependencyRows, platformTopologyGraph });
