// pages/VulnerabilitiesReachable.tsx
// Advisories correlated to reachable services via the impact findings surface
// (GET /api/v0/supply-chain/impact/findings). This is admitted impact truth, as
// opposed to the broader known-intelligence catalog.
import { Link } from "react-router-dom";

import { AsyncStateGuard } from "../components/AsyncStateGuard";
import { Panel, StatTile, Badge } from "../components/atoms";
import { Donut } from "../components/charts";
import { GraphCanvas } from "../components/GraphCanvas";
import type { ConsoleModel, GraphModel, Severity, VulnRow } from "../console/types";
import { SEVERITY_COLOR } from "../console/types";
import "./supplyChainImpactPath.css";

type ImpactImage = ConsoleModel["images"][number];

interface PathHop {
  readonly id: string;
  readonly label: string;
  readonly detail: string;
  readonly state: "exact" | "derived" | "missing" | "stale";
  readonly type: "advisory" | "image" | "owner" | "package" | "sbom" | "service" | "workload";
}

export function ReachableAdvisories({
  model,
}: {
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const rows = model.vulnerabilities.slice().sort((a, b) => b.cvss - a.cvss);
  const sevCount: Record<Severity, number> = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  rows.forEach((v) => {
    const k = v.severity as Severity;
    if (k in sevCount) sevCount[k] += 1;
  });
  const kev = rows.filter((v) => v.kev).length;
  const fixable = rows.filter((v) => v.fixedVersion).length;
  const provenance =
    model.provenance.vulnerabilities ?? (model.source === "demo" ? "demo" : "loading");
  return (
    <div>
      <p className="t-mut" style={{ fontSize: ".82rem", margin: "0 0 var(--gap)" }}>
        Reachable advisories —{" "}
        <span className="mono">GET /api/v0/supply-chain/impact/findings</span>.
      </p>
      <div className="grid g-4">
        <StatTile
          label="Open advisories"
          value={rows.length}
          color="var(--crit)"
          sub={`${sevCount.critical} critical · ${sevCount.high} high`}
        />
        <StatTile label="KEV-listed" value={kev} color="var(--crit)" sub="known exploited" />
        <StatTile
          label="Fix available"
          value={`${fixable}/${rows.length || 0}`}
          color="var(--teal)"
          sub="patch path exists"
        />
        <StatTile
          label="Source"
          value={model.source === "live" ? "live" : "demo"}
          color="var(--ember)"
          sub="impact findings"
        />
      </div>
      <SupplyChainImpactPath model={model} row={rows[0] ?? null} />
      <div className="grid mt supply-chain-register-grid">
        <Panel title="By severity">
          <div style={{ display: "grid", placeItems: "center" }}>
            <Donut
              size={138}
              thickness={17}
              center={{ value: rows.length, label: "advisories" }}
              segments={(["critical", "high", "medium", "low"] as const).map((k) => ({
                label: k,
                value: sevCount[k],
                color: SEVERITY_COLOR[k],
              }))}
            />
          </div>
        </Panel>
        <Panel className="flush" title="Advisory register" sub="Sorted by CVSS">
          <div className="supply-chain-register-scroll">
            <AsyncStateGuard provenance={provenance} label="vulnerabilities">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Severity</th>
                    <th>CVSS</th>
                    <th>Package</th>
                    <th>Services</th>
                    <th>Fix</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((v) => (
                    <tr key={v.id}>
                      <td className="row" style={{ gap: 7 }}>
                        <Link
                          to={`/vulnerabilities/${encodeURIComponent(v.id)}`}
                          className="t-name link-btn"
                          style={{ fontSize: ".8rem" }}
                        >
                          {v.id}
                        </Link>
                        {v.kev ? <span className="kev-flag">KEV</span> : null}
                      </td>
                      <td>
                        <span
                          className="sev-tag"
                          style={{
                            color:
                              SEVERITY_COLOR[
                                (v.severity as Severity) in SEVERITY_COLOR
                                  ? (v.severity as Severity)
                                  : "medium"
                              ],
                          }}
                        >
                          <i style={{ background: "currentColor" }} />
                          {v.severity}
                        </span>
                      </td>
                      <td className="mono" style={{ fontSize: ".82rem" }}>
                        {v.cvss || "—"}
                      </td>
                      <td className="t-mut mono" style={{ fontSize: ".78rem" }}>
                        {v.package}
                      </td>
                      <td className="t-mut" style={{ fontSize: ".76rem" }}>
                        {v.services.slice(0, 2).join(", ")}
                        {v.services.length > 2 ? ` +${v.services.length - 2}` : ""}
                      </td>
                      <td>
                        {v.fixedVersion ? (
                          <Badge tone="teal">{v.fixedVersion}</Badge>
                        ) : (
                          <Badge tone="crit">none</Badge>
                        )}
                      </td>
                    </tr>
                  ))}
                  {rows.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="empty">
                        No affected service proven by current vulnerability impact evidence.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </AsyncStateGuard>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function SupplyChainImpactPath({
  model,
  row,
}: {
  readonly model: ConsoleModel;
  readonly row: VulnRow | null;
}): React.JSX.Element {
  const path = buildImpactPath(model, row);
  return (
    <section className="supply-chain-path mt" aria-label="Supply-chain impact path">
      <Panel
        className="flush"
        title="Supply-chain impact path"
        sub={row === null ? "no admitted impact" : "advisory to runtime ownership"}
      >
        {row === null ? (
          <div className="supply-chain-path-empty">
            <strong>No admitted supply-chain impact path</strong>
            <p>No affected service has been proven by vulnerability impact evidence.</p>
          </div>
        ) : (
          <>
            <div className="supply-chain-path-grid">
              {path.hops.map((hop, index) => (
                <article className={`supply-chain-hop is-${hop.state}`} key={hop.id}>
                  <span className="supply-chain-hop-index">{index + 1}</span>
                  <div>
                    <strong>{hop.label}</strong>
                    <small>{hop.detail}</small>
                  </div>
                  <Badge
                    tone={
                      hop.state === "missing" || hop.state === "stale"
                        ? "warn"
                        : hop.state === "exact"
                          ? "teal"
                          : "neutral"
                    }
                  >
                    {hop.state === "missing" ? "not proven" : hop.state}
                  </Badge>
                </article>
              ))}
            </div>
            <GraphCanvas graph={path.graph} height={280} />
            <div className="supply-chain-path-links" aria-label="Evidence pivots">
              <Link className="link-btn" to={`/vulnerabilities/${encodeURIComponent(row.id)}`}>
                Raw advisory evidence
              </Link>
              <Link className="link-btn" to="/dependencies">
                Dependency graph
              </Link>
              <Link className="link-btn" to="/sbom">
                SBOM evidence
              </Link>
              <Link className="link-btn" to="/images">
                Image inventory
              </Link>
              <span className="mono">
                MCP/API workflow: get impact findings → resolve citations → derive visualization
                packet
              </span>
            </div>
          </>
        )}
      </Panel>
    </section>
  );
}

function buildImpactPath(
  model: ConsoleModel,
  row: VulnRow | null,
): { readonly graph: GraphModel; readonly hops: readonly PathHop[] } {
  if (row === null) return { graph: { edges: [], nodes: [] }, hops: [] };
  const dependency = model.dependencies.find(
    (dep) =>
      samePackage(dep.anchorPackage, row.package) || samePackage(dep.relatedPackage, row.package),
  );
  const serviceName = row.services[0] ?? "";
  const image = imageForImpactPath(model, serviceName);
  const workload = workloadForService(model, serviceName);
  const sbomDetail =
    model.sbom === null || model.sbom.total === 0
      ? "SBOM evidence missing"
      : "SBOM correlation missing";
  const imageMissing = image === null;
  const hops: readonly PathHop[] = [
    {
      detail: "admitted impact",
      id: "advisory",
      label: row.id,
      state: "exact",
      type: "advisory",
    },
    {
      detail: dependency
        ? `${dependency.ecosystem || "package"} ${dependency.declaringVersion || dependency.range || ""}`.trim()
        : "package evidence from impact finding",
      id: "package",
      label: row.package,
      state: dependency ? "exact" : "derived",
      type: "package",
    },
    {
      detail: sbomDetail,
      id: "sbom",
      label: "SBOM",
      state: "missing",
      type: "sbom",
    },
    {
      detail: imageMissing
        ? "Image evidence missing"
        : image.digest || image.repository || "image identity",
      id: "image",
      label: imageMissing ? "Image" : shortDigest(image.digest || image.id),
      state: imageMissing ? "missing" : "exact",
      type: "image",
    },
    {
      detail: workload === "" ? "workload evidence missing" : "runtime placement",
      id: "workload",
      label: workload || "Workload",
      state: workload === "" ? "missing" : "derived",
      type: "workload",
    },
    {
      detail: serviceName === "" ? "service evidence missing" : "reachable service",
      id: "service",
      label: serviceName || "Service",
      state: serviceName === "" ? "missing" : "exact",
      type: "service",
    },
    {
      detail: "Owner evidence missing",
      id: "owner",
      label: "Owner",
      state: "missing",
      type: "owner",
    },
  ];
  return { graph: graphFromHops(hops), hops };
}

function graphFromHops(hops: readonly PathHop[]): GraphModel {
  return {
    edges: hops.slice(1).map((hop, index) => ({
      layer: edgeLayer(hop.type),
      s: hops[index].id,
      t: hop.id,
      verb: hop.state === "missing" ? "MISSING" : edgeVerb(hop.type),
    })),
    nodes: hops.map((hop, index) => ({
      col: index,
      hero: index === 0,
      id: hop.id,
      kind: graphKind(hop.type),
      label: hop.label,
      sub: hop.detail,
      truth: hop.state === "exact" ? "exact" : hop.state === "derived" ? "derived" : "inferred",
    })),
  };
}

function edgeLayer(type: PathHop["type"]): GraphModel["edges"][number]["layer"] {
  if (type === "advisory" || type === "package" || type === "sbom") return "security";
  if (type === "image") return "deploy";
  if (type === "workload" || type === "service") return "runtime";
  return "ops";
}

function edgeVerb(type: PathHop["type"]): string {
  if (type === "package") return "AFFECTS";
  if (type === "sbom") return "IN_SBOM";
  if (type === "image") return "ATTACHED_TO";
  if (type === "workload") return "RUNS_AS";
  if (type === "service") return "SERVES";
  return "OWNED_BY";
}

function graphKind(type: PathHop["type"]): string {
  if (type === "advisory") return "vuln";
  if (type === "package") return "library";
  return type;
}

function samePackage(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase();
}

function workloadForService(model: ConsoleModel, serviceName: string): string {
  const service = model.graph.nodes.find(
    (node) => node.kind === "service" && node.label === serviceName,
  );
  if (service === undefined) return "";
  const edge = model.graph.edges.find(
    (candidate) =>
      candidate.verb === "RUNS_AS" && (candidate.s === service.id || candidate.t === service.id),
  );
  const workloadId = edge?.s === service.id ? edge.t : edge?.s;
  const workload = model.graph.nodes.find((node) => node.id === workloadId);
  if (workload === undefined) return "";
  if (workload.id.startsWith("wl:")) {
    return `workload:${workload.id.slice("wl:".length)}`;
  }
  return workload.label || workload.id;
}

function imageForImpactPath(model: ConsoleModel, serviceName: string): ImpactImage | null {
  const service = model.graph.nodes.find(
    (node) => node.kind === "service" && node.label === serviceName,
  );
  if (service === undefined) return null;
  const imageNode = model.graph.nodes.find(
    (node) =>
      node.kind === "image" &&
      model.graph.edges.some(
        (edge) => edge.verb === "DEPLOYS_FROM" && connects(edge, service.id, node.id),
      ),
  );
  if (imageNode === undefined) return null;
  return model.images.find((image) => imageMatchesGraphNode(image, imageNode)) ?? null;
}

function connects(edge: GraphModel["edges"][number], a: string, b: string): boolean {
  return (edge.s === a && edge.t === b) || (edge.s === b && edge.t === a);
}

function imageMatchesGraphNode(
  image: ImpactImage,
  imageNode: GraphModel["nodes"][number],
): boolean {
  const targets = [imageNode.id, imageNode.label].map(matchKey).filter((value) => value.length > 0);
  const exactCandidates = [image.id, image.digest, image.repositoryId]
    .map(matchKey)
    .filter((value) => value.length > 0);
  const tagCandidates = [
    image.tag === "" ? "" : `${image.name}:${image.tag}`,
    image.tag === "" ? "" : `${image.repository}:${image.tag}`,
  ]
    .map(matchKey)
    .filter((value) => value.length > 0);
  const repositoryCandidates = [image.repository, image.name]
    .map(matchKey)
    .filter((value) => value.length > 0);
  return targets.some(
    (target) =>
      exactCandidates.some(
        (candidate) => target.includes(candidate) || candidate.includes(target),
      ) ||
      tagCandidates.some((candidate) => target === candidate || target.endsWith(candidate)) ||
      (!target.includes(":") &&
        repositoryCandidates.some(
          (candidate) => target === candidate || target.endsWith(candidate),
        )),
  );
}

function matchKey(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9:]/g, "");
}

function shortDigest(value: string): string {
  if (value === "") return "image";
  if (!value.startsWith("sha256:")) return value.length > 18 ? `${value.slice(0, 18)}...` : value;
  const body = value.slice("sha256:".length);
  return body.length > 12 ? `sha256:${body.slice(0, 12)}` : value;
}
