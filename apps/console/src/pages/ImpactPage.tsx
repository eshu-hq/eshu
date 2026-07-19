import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { DeployableUnitPacketPanel, packetFormFromSearch } from "./DeployableUnitPacketPanel";
import { DeploymentTraceSummary, ImpactGraphProvenance } from "./ImpactDeploymentSummary";
import { ImpactSelectedEdges } from "./ImpactSelectedEdges";
import { useImpactReviewLifecycle } from "./useImpactReviewLifecycle";
import type { ChangeSurfaceInvestigation } from "../api/changeSurface";
import type { EshuApiClient } from "../api/client";
import { demoDefaults } from "../api/demoClient";
import type { EshuTruth } from "../api/envelope";
import type { ImpactReview, ImpactSection, ImpactTargetKind } from "../api/impactReviewTypes";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";
import { defaultServiceName } from "../console/defaultEntity";
import type { ConsoleModel } from "../console/types";
import { fmt, uiFresh, uiTruth } from "../console/types";
import "./impactPage.css";

interface ImpactFormState {
  readonly environment: string;
  readonly kind: ImpactTargetKind;
  readonly repoId: string;
  readonly target: string;
}

const targetKinds: readonly { readonly label: string; readonly value: ImpactTargetKind }[] = [
  { label: "Service", value: "service" },
  { label: "Workload", value: "workload" },
  { label: "Repository", value: "repository" },
  { label: "Cloud resource", value: "resource" },
  { label: "Terraform module", value: "terraform_module" },
  { label: "Code topic", value: "code_topic" },
  { label: "SQL table", value: "sql_table" },
  { label: "Crossplane XRD", value: "crossplane_xrd" },
];

export function ImpactPage({
  client,
  model,
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const demoMode = model.source === "demo";
  // Auto-load a sensible default on open: in live mode with no explicit target,
  // seed a real service from the catalog so the page renders a blast/change graph
  // immediately instead of an empty form. The query form still overrides.
  const liveDefaultTarget = demoMode ? "" : defaultServiceName(model);
  const [form, setForm] = useState<ImpactFormState>(() =>
    formFromSearch(searchParams, demoMode, liveDefaultTarget),
  );
  const [formError, setFormError] = useState("");
  const { busy, error, load, review, selectNode, selectedNode } = useImpactReviewLifecycle(client);
  const canLoad = (model.source === "live" || demoMode) && client !== undefined;
  const deployablePacketInitial = useMemo(() => packetFormFromSearch(searchParams), [searchParams]);

  useEffect(() => {
    const next = formFromSearch(searchParams, demoMode, liveDefaultTarget);
    setForm(next);
    if (canLoad && next.target.trim().length > 0) {
      load({
        environment: next.environment,
        repoId: next.repoId,
        target: next.target.trim(),
        targetKind: next.kind,
      });
    }
  }, [canLoad, demoMode, liveDefaultTarget, load, searchParams]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const target = form.target.trim();
    if (target.length === 0) {
      setFormError("Entity target is required.");
      return;
    }
    setFormError("");
    const params = new URLSearchParams({
      kind: form.kind,
      target,
    });
    if (form.repoId.trim().length > 0) {
      params.set("repoId", form.repoId.trim());
    }
    if (form.environment.trim().length > 0) {
      params.set("environment", form.environment.trim());
    }
    setSearchParams(params);
  }

  const graph = review?.graph ?? { edges: [], nodes: [] };
  const stats = useMemo(() => statRows(review), [review]);
  const selectedEdges = useMemo(
    () =>
      selectedNode === undefined
        ? []
        : graph.edges.filter((edge) => edge.s === selectedNode.id || edge.t === selectedNode.id),
    [graph.edges, selectedNode],
  );

  return (
    <div className="page impact-page" style={{ maxWidth: "none" }}>
      <div className="page-intro impact-intro">
        <div>
          <h2>Impact</h2>
        </div>
        <Badge tone={canLoad ? "teal" : "warn"}>
          {canLoad ? (demoMode ? "demo fixtures" : "live API") : "connect live API"}
        </Badge>
      </div>

      <form className="impact-query" onSubmit={submit}>
        <label>
          <span>Entity type</span>
          <select
            aria-label="Entity type"
            className="popover-input mono"
            value={form.kind}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                kind: event.target.value as ImpactTargetKind,
              }))
            }
          >
            {targetKinds.map((kind) => (
              <option key={kind.value} value={kind.value}>
                {kind.label}
              </option>
            ))}
          </select>
        </label>
        <label className="impact-query-target">
          <span>Target</span>
          <input
            aria-label="Entity target"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, target: event.target.value }))}
            placeholder="catalog-api, repository:r_..., module source"
            value={form.target}
          />
        </label>
        <label>
          <span>Repo scope</span>
          <input
            aria-label="Repository scope"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, repoId: event.target.value }))}
            placeholder="optional repo_id"
            value={form.repoId}
          />
        </label>
        <label>
          <span>Environment</span>
          <input
            aria-label="Environment"
            className="popover-input mono"
            onChange={(event) =>
              setForm((current) => ({ ...current, environment: event.target.value }))
            }
            placeholder="optional"
            value={form.environment}
          />
        </label>
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Loading..." : "Review impact"}
        </button>
      </form>

      {!canLoad ? (
        <p className="inline-state">
          {demoMode ? "Demo fixture client unavailable." : "Live Eshu API connection unavailable."}
        </p>
      ) : null}
      {error || formError ? <p className="src-err">{error || formError}</p> : null}

      <div className="grid g-4 mt">
        {stats.map((stat) => (
          <StatTile
            color={stat.color}
            key={stat.label}
            label={stat.label}
            sub={stat.sub}
            value={stat.value}
          />
        ))}
      </div>

      <div className="impact-layout mt">
        <Panel
          className="flush impact-graph-panel"
          sub={
            review
              ? `${review.input.targetKind.replace(/_/g, " ")} · ${review.input.target}`
              : "No impact graph loaded"
          }
          title={review?.graphPresentation.title ?? "Impact graph"}
        >
          {review ? <ImpactGraphProvenance presentation={review.graphPresentation} /> : null}
          {busy ? (
            <div className="conn-state compact">
              <div aria-hidden className="conn-spinner" />
              <p>Loading impact review...</p>
            </div>
          ) : (
            <GraphCanvas
              graph={graph}
              height={590}
              layout="layered"
              onSelect={selectNode}
              selectedId={selectedNode?.id}
            />
          )}
        </Panel>
        <Panel title="Selected entity">
          {selectedNode !== undefined ? (
            <div className="impact-inspector">
              <div className="insp-head">
                <span className="cglyph">{selectedNode.kind.slice(0, 2).toUpperCase()}</span>
                <div>
                  <div className="insp-kind">{selectedNode.kind}</div>
                  <div className="insp-title">{selectedNode.label}</div>
                </div>
              </div>
              <p className="mono t-mut">{selectedNode.id}</p>
              {selectedNode.sub ? <p className="mono t-mut">{selectedNode.sub}</p> : null}
              {selectedNode.truth ? <TruthChip level={selectedNode.truth} /> : null}
              <div className="section-label">Impact edges</div>
              <ImpactSelectedEdges
                edges={selectedEdges}
                nodes={graph.nodes}
                selectedID={selectedNode.id}
              />
            </div>
          ) : (
            <p className="empty">No selected entity.</p>
          )}
        </Panel>
      </div>

      <div className="impact-evidence-grid mt">
        <ImpactSectionPanel section={review?.blast ?? null} title="Blast radius">
          {review?.blast.status === "ready" ? (
            <EntityList
              empty="No transitive dependents returned."
              rows={review.blast.data.affected.map((entity) => ({
                detail: [entity.repoId, entity.tier, entity.risk, `hop ${entity.hops}`]
                  .filter(Boolean)
                  .join(" · "),
                name: entity.repo,
              }))}
            />
          ) : null}
        </ImpactSectionPanel>

        <ImpactSectionPanel section={review?.changeSurface ?? null} title="Change surface">
          {review?.changeSurface.status === "ready" ? (
            <ChangeSurfaceSummary investigation={review.changeSurface.data} />
          ) : null}
        </ImpactSectionPanel>

        <ImpactSectionPanel section={review?.deploymentTrace ?? null} title="Deployment chain">
          {review?.deploymentTrace.status === "ready" ? (
            <DeploymentTraceSummary
              canInspectEntity={(entityId) =>
                graph.nodes.some((candidate) => candidate.id === entityId)
              }
              onInspectEntity={(entityId) => {
                const node = graph.nodes.find((candidate) => candidate.id === entityId);
                if (node !== undefined) selectNode(node);
              }}
              trace={review.deploymentTrace.data}
            />
          ) : null}
        </ImpactSectionPanel>
      </div>

      <DeployableUnitPacketPanel
        canLoad={canLoad}
        client={client}
        initial={deployablePacketInitial}
      />
    </div>
  );
}

// formFromSearch reads the impact query form from URL params. `liveDefaultTarget`
// seeds the target for the live page open state (no demo, no explicit target) so
// the page auto-loads a real catalog service instead of an empty form; an
// explicit `target` param always wins.
function formFromSearch(
  searchParams: URLSearchParams,
  demoMode = false,
  liveDefaultTarget = "",
): ImpactFormState {
  const kind = searchParams.get("kind");
  const target =
    searchParams.get("target") ?? (demoMode ? demoDefaults.impact.target : liveDefaultTarget);
  return {
    environment:
      searchParams.get("environment") ?? (demoMode ? demoDefaults.impact.environment : ""),
    kind: kind === null && demoMode ? demoDefaults.impact.kind : parseTargetKind(kind),
    repoId: searchParams.get("repoId") ?? "",
    target,
  };
}

function parseTargetKind(raw: string | null): ImpactTargetKind {
  const values = new Set(targetKinds.map((kind) => kind.value));
  return raw !== null && values.has(raw as ImpactTargetKind)
    ? (raw as ImpactTargetKind)
    : "service";
}

function statRows(review: ImpactReview | null): readonly {
  readonly color: string;
  readonly label: string;
  readonly sub: string;
  readonly value: string | number;
}[] {
  if (review === null) {
    return [
      { color: "var(--teal)", label: "Graph nodes", sub: "awaiting review", value: "—" },
      { color: "var(--blue)", label: "Change surface", sub: "awaiting review", value: "—" },
      { color: "var(--ember)", label: "Blast radius", sub: "awaiting review", value: "—" },
      { color: "var(--violet)", label: "Deployment chain", sub: "awaiting review", value: "—" },
    ];
  }
  const change = review.changeSurface.status === "ready" ? review.changeSurface.data : null;
  const blast = review.blast.status === "ready" ? review.blast.data : null;
  const trace = review.deploymentTrace.status === "ready" ? review.deploymentTrace.data : null;
  return [
    {
      color: "var(--teal)",
      label: "Graph nodes",
      sub: `${review.graph.edges.length} edges`,
      value: fmt(review.graph.nodes.length),
    },
    {
      color: "var(--blue)",
      label: "Change surface",
      sub: change?.truncated ? "truncated" : "bounded",
      value: change?.impact.totalCount ?? "—",
    },
    {
      color: "var(--ember)",
      label: "Blast radius",
      sub: review.blast.status,
      value: blast?.affectedCount ?? "—",
    },
    {
      color: "var(--violet)",
      label: "Deployment chain",
      sub: review.deploymentTrace.status,
      value:
        trace === null
          ? "—"
          : trace.deploymentSources.length +
            trace.cloudResources.length +
            trace.k8sResources.length,
    },
  ];
}

function ImpactSectionPanel<TData>({
  children,
  section,
  title,
}: {
  readonly children: React.ReactNode;
  readonly section: ImpactSection<TData> | null;
  readonly title: string;
}): React.JSX.Element {
  return (
    <Panel title={title}>
      <div className="impact-section-head">
        <span className={`impact-status impact-status-${section?.status ?? "idle"}`}>
          {section?.status ?? "idle"}
        </span>
        {section?.status === "ready" ? <TruthSummary truth={section.truth} /> : null}
      </div>
      {section === null ? <p className="empty">No route evidence loaded.</p> : null}
      {section?.status === "skipped" ? <p className="inline-state">{section.reason}</p> : null}
      {section?.status === "unavailable" ? <p className="src-err">{section.error}</p> : null}
      {section?.status === "ready" ? (
        <>
          <p className="mono impact-source">{section.source}</p>
          {children}
        </>
      ) : null}
    </Panel>
  );
}

function TruthSummary({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  if (truth === null) {
    return <span className="t-mut">truth envelope unavailable</span>;
  }
  return (
    <span className="impact-truth">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </span>
  );
}

function ChangeSurfaceSummary({
  investigation,
}: {
  readonly investigation: ChangeSurfaceInvestigation;
}): React.JSX.Element {
  return (
    <div className="impact-summary-block">
      <div className="impact-mini-stats">
        <span>{investigation.impact.directCount} direct</span>
        <span>{investigation.impact.transitiveCount} deeper</span>
        <span>{investigation.coverage.queryShape.replace(/_/g, " ")}</span>
      </div>
      <EntityList
        empty="No affected entities returned."
        rows={[...investigation.directImpact, ...investigation.transitiveImpact].map((node) => ({
          detail: [node.repoId, node.environment, `depth ${node.depth}`]
            .filter(Boolean)
            .join(" · "),
          name: node.name,
        }))}
      />
      {investigation.codeSurface.symbols.length > 0 ? (
        <>
          <div className="section-label">Code surface</div>
          <EntityList
            empty="No touched symbols returned."
            rows={investigation.codeSurface.symbols.slice(0, 5).map((symbol) => ({
              detail: symbol.relativePath,
              name: symbol.name,
            }))}
          />
        </>
      ) : null}
    </div>
  );
}

function EntityList({
  empty,
  rows,
}: {
  readonly empty: string;
  readonly rows: readonly { readonly detail?: string; readonly name: string }[];
}): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">{empty}</p>;
  }
  return (
    <div className="impact-entity-list">
      {rows.map((row, index) => (
        <article key={`${row.name}:${row.detail ?? ""}:${index}`}>
          <strong>{row.name}</strong>
          {row.detail ? <span>{row.detail}</span> : null}
        </article>
      ))}
    </div>
  );
}
