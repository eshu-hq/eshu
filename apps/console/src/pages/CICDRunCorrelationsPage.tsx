import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";

import {
  loadCICDRunCorrelationReview,
  type CICDReviewSection,
  type CICDRunCorrelationCount,
  type CICDRunCorrelationInventory,
  type CICDRunCorrelationPage,
  type CICDRunCorrelationReview,
  type CICDRunCorrelationRow
} from "../api/cicdRunCorrelations";
import type { EshuApiClient } from "../api/client";
import { demoDefaults } from "../api/demoClient";
import type { EshuTruth } from "../api/envelope";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import type { ConsoleModel } from "../console/types";
import { fmt, uiFresh, uiTruth } from "../console/types";
import "./cicdRunCorrelationsPage.css";

interface FormState {
  readonly artifactDigest: string;
  readonly commitSha: string;
  readonly environment: string;
  readonly imageRef: string;
  readonly limit: string;
  readonly outcome: string;
  readonly provider: string;
  readonly providerRunId: string;
  readonly repositoryId: string;
  readonly scopeId: string;
}

export function CICDRunCorrelationsPage({
  client,
  model
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const demoMode = model.source === "demo";
  const [form, setForm] = useState<FormState>(() => formFromSearch(searchParams, demoMode));
  const [review, setReview] = useState<CICDRunCorrelationReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canLoad = (model.source === "live" || demoMode) && client !== undefined;

  const runReview = useCallback(
    async (next: FormState) => {
      if (!client) return;
      setBusy(true);
      setError("");
      try {
        const loaded = await loadCICDRunCorrelationReview(client, {
          artifactDigest: next.artifactDigest,
          commitSha: next.commitSha,
          environment: next.environment,
          imageRef: next.imageRef,
          limit: Number(next.limit),
          outcome: next.outcome,
          provider: next.provider,
          providerRunId: next.providerRunId,
          repositoryId: next.repositoryId,
          scopeId: next.scopeId
        });
        setReview(loaded);
      } catch (runError) {
        setReview(null);
        setError(runError instanceof Error ? runError.message : "failed to load CI/CD run correlations");
      } finally {
        setBusy(false);
      }
    },
    [client]
  );

  useEffect(() => {
    const next = formFromSearch(searchParams, demoMode);
    setForm(next);
    if (canLoad) {
      void runReview(next);
    }
  }, [canLoad, demoMode, runReview, searchParams]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const params = new URLSearchParams();
    addParam(params, "scope_id", form.scopeId);
    addParam(params, "repository_id", form.repositoryId);
    addParam(params, "commit_sha", form.commitSha);
    addParam(params, "provider", form.provider);
    addParam(params, "provider_run_id", form.providerRunId);
    addParam(params, "artifact_digest", form.artifactDigest);
    addParam(params, "image_ref", form.imageRef);
    addParam(params, "environment", form.environment);
    addParam(params, "outcome", form.outcome);
    if (form.limit.trim().length > 0 && form.limit.trim() !== "25") {
      params.set("limit", form.limit.trim());
    }
    setSearchParams(params);
  }

  const stats = useMemo(() => statRows(review), [review]);
  const list = review?.list.status === "ready" ? review.list.data : null;
  const count = review?.count.status === "ready" ? review.count.data : null;
  const inventory = review?.inventory.status === "ready" ? review.inventory.data : null;

  return (
    <div className="page cicd-page" style={{ maxWidth: "none" }}>
      <div className="page-intro cicd-intro">
        <h2>CI/CD run correlations</h2>
        <Badge tone={canLoad ? "teal" : "warn"}>{canLoad ? demoMode ? "demo fixtures" : "live API" : "connect live API"}</Badge>
      </div>

      <form className="cicd-query" onSubmit={submit}>
        <FilterInput label="Scope id" value={form.scopeId} onChange={(value) => setForm((current) => ({ ...current, scopeId: value }))} />
        <FilterInput className="cicd-query-wide" label="Repository id" value={form.repositoryId} onChange={(value) => setForm((current) => ({ ...current, repositoryId: value }))} />
        <FilterInput label="Commit sha" value={form.commitSha} onChange={(value) => setForm((current) => ({ ...current, commitSha: value }))} />
        <FilterInput label="Provider" value={form.provider} onChange={(value) => setForm((current) => ({ ...current, provider: value }))} />
        <FilterInput label="Provider run id" value={form.providerRunId} onChange={(value) => setForm((current) => ({ ...current, providerRunId: value }))} />
        <FilterInput className="cicd-query-wide" label="Artifact digest" value={form.artifactDigest} onChange={(value) => setForm((current) => ({ ...current, artifactDigest: value }))} />
        <FilterInput className="cicd-query-wide" label="Image ref" value={form.imageRef} onChange={(value) => setForm((current) => ({ ...current, imageRef: value }))} />
        <FilterInput label="Environment" value={form.environment} onChange={(value) => setForm((current) => ({ ...current, environment: value }))} />
        <FilterInput label="Outcome" value={form.outcome} onChange={(value) => setForm((current) => ({ ...current, outcome: value }))} />
        <FilterInput label="Limit" value={form.limit} onChange={(value) => setForm((current) => ({ ...current, limit: value }))} />
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Loading..." : "Review runs"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">{demoMode ? "Demo fixture client unavailable." : "Live Eshu API connection unavailable."}</p> : null}
      {error ? <p className="src-err">{error}</p> : null}

      <div className="grid g-4 mt">
        {stats.map((stat) => (
          <StatTile color={stat.color} key={stat.label} label={stat.label} sub={stat.sub} value={stat.value} />
        ))}
      </div>

      <div className="cicd-summary-grid mt">
        <Panel title="Aggregate truth" sub="Cheap count and inventory endpoints">
          <SectionStatus section={review?.count ?? null} />
          {count ? <RollupGrid count={count} /> : <p className="empty">No aggregate count loaded.</p>}
        </Panel>
        <Panel title="Inventory buckets" sub={inventory ? `grouped by ${inventory.groupBy}` : "grouped by outcome"}>
          <SectionStatus section={review?.inventory ?? null} />
          {inventory ? <BucketList inventory={inventory} /> : <p className="empty">No inventory loaded.</p>}
        </Panel>
      </div>

      <div className="cicd-detail-grid mt">
        <Panel title="Run correlations" sub={list ? `${list.count} rows | limit ${list.limit}` : "bounded list"}>
          <ListStatus review={review} />
          {list ? <CorrelationTable rows={list.correlations} /> : null}
        </Panel>
        <Panel title="Evidence summary" sub="Workflow, live run, and artifact bridge evidence">
          {list ? <EvidenceSummary page={list} /> : <p className="empty">Add an anchor to load row evidence.</p>}
        </Panel>
      </div>
    </div>
  );
}

function FilterInput({
  className,
  label,
  onChange,
  value
}: {
  readonly className?: string;
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}): React.JSX.Element {
  return (
    <label className={className}>
      <span>{label}</span>
      <input
        aria-label={label}
        className="popover-input mono"
        onChange={(event) => onChange(event.target.value)}
        placeholder="optional"
        value={value}
      />
    </label>
  );
}

function SectionStatus<TData>({ section }: { readonly section: CICDReviewSection<TData> | null }): React.JSX.Element | null {
  if (section === null) return null;
  if (section.status === "unavailable") {
    return <p className="src-err">{section.error}</p>;
  }
  return <TruthSummary truth={section.truth} />;
}

function ListStatus({ review }: { readonly review: CICDRunCorrelationReview | null }): React.JSX.Element | null {
  if (review === null) return <p className="empty">No CI/CD run correlation review loaded.</p>;
  if (review.list.status === "skipped") return <p className="inline-state">{review.list.reason}</p>;
  if (review.list.status === "unavailable") return <p className="src-err">{review.list.error}</p>;
  return <TruthSummary truth={review.list.truth} />;
}

function TruthSummary({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  if (truth === null) {
    return <span className="t-mut">truth envelope unavailable</span>;
  }
  return (
    <span className="cicd-truth">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </span>
  );
}

function RollupGrid({ count }: { readonly count: CICDRunCorrelationCount }): React.JSX.Element {
  return (
    <div className="cicd-rollups">
      <Rollup title="Outcome" values={count.byOutcome} />
      <Rollup title="Environment" values={count.byEnvironment} />
      <Rollup title="Provider" values={count.byProvider} />
    </div>
  );
}

function Rollup({ title, values }: { readonly title: string; readonly values: Record<string, number> }): React.JSX.Element {
  const rows = Object.entries(values).sort((a, b) => b[1] - a[1]);
  return (
    <div className="cicd-rollup">
      <strong>{title}</strong>
      {rows.map(([key, value]) => (
        <span key={key}>{formatLabel(key)} <b>{fmt(value)}</b></span>
      ))}
      {rows.length === 0 ? <span>no buckets</span> : null}
    </div>
  );
}

function BucketList({ inventory }: { readonly inventory: CICDRunCorrelationInventory }): React.JSX.Element {
  if (inventory.buckets.length === 0) {
    return <p className="empty">No inventory buckets returned.</p>;
  }
  return (
    <div className="cicd-buckets">
      {inventory.buckets.map((bucket) => (
        <div key={`${bucket.dimension}:${bucket.value}`}>
          <strong>{formatLabel(bucket.value)}</strong>
          <span>{fmt(bucket.count)} rows</span>
        </div>
      ))}
    </div>
  );
}

function CorrelationTable({ rows }: { readonly rows: readonly CICDRunCorrelationRow[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No run correlations match this bounded scope.</p>;
  }
  return (
    <div className="cicd-table-wrap">
      <table className="tbl cicd-table">
        <thead>
          <tr>
            <th>Run</th>
            <th>Scope</th>
            <th>Artifact</th>
            <th>Truth</th>
            <th>Links</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.correlationId}>
              <td><CellStack title={row.runId || row.correlationId} sub={[row.provider, row.correlationKind].filter(Boolean).join(" | ")} /></td>
              <td><CellStack title={row.repositoryId || "repository unknown"} sub={[row.environment, shortSha(row.commitSha)].filter(Boolean).join(" | ")} /></td>
              <td><CellStack title={shortDigest(row.artifactDigest) || row.imageRef || "no artifact"} sub={row.reason} /></td>
              <td><span className={`cicd-outcome cicd-outcome-${classToken(row.outcome)}`}>{formatLabel(row.outcome)}</span></td>
              <td><RowLinks row={row} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CellStack({ sub, title }: { readonly sub: string; readonly title: string }): React.JSX.Element {
  return (
    <span className="cell-stack">
      <span className="t-name">{title}</span>
      {sub ? <small className="mono">{sub}</small> : null}
    </span>
  );
}

function RowLinks({ row }: { readonly row: CICDRunCorrelationRow }): React.JSX.Element {
  return (
    <span className="cicd-links">
      {row.repositoryId ? (
        <Link className="btn-ghost" to={`/repositories/${encodeURIComponent(row.repositoryId)}/source`}>Repository</Link>
      ) : null}
      {row.canonicalTarget ? (
        <Link className="btn-ghost" to={`/impact?kind=service&target=${encodeURIComponent(row.canonicalTarget)}`}>Impact</Link>
      ) : null}
    </span>
  );
}

function EvidenceSummary({ page }: { readonly page: CICDRunCorrelationPage }): React.JSX.Element {
  const summary = page.evidenceSummary;
  return (
    <div className="cicd-evidence">
      <EvidenceBlock
        count={summary.staticWorkflowArtifacts.count}
        label="Static workflow artifacts"
        state={summary.staticWorkflowArtifacts.state}
        sub={formatLabel(summary.staticWorkflowArtifacts.evidenceClass || summary.staticWorkflowArtifacts.reason)}
      />
      <EvidenceBlock count={summary.liveRunCorrelations.count} label="Live run correlations" state={summary.liveRunCorrelations.state} sub={summary.liveRunCorrelations.reason} />
      <EvidenceBlock count={summary.runArtifactEvidence.count} label="Run artifact evidence" state={summary.runArtifactEvidence.state} sub={`${summary.runArtifactEvidence.artifactDigestCount} digests | ${summary.runArtifactEvidence.imageRefCount} image refs`} />
      <div className="cicd-missing">
        <strong>Missing evidence</strong>
        {summary.missingEvidence.map((gap) => (
          <span key={gap}>{formatLabel(gap)}</span>
        ))}
        {summary.missingEvidence.length === 0 ? <span>no missing-hop classes</span> : null}
      </div>
    </div>
  );
}

function EvidenceBlock({
  count,
  label,
  state,
  sub
}: {
  readonly count: number;
  readonly label: string;
  readonly state: string;
  readonly sub: string;
}): React.JSX.Element {
  return (
    <div className="cicd-evidence-block">
      <span>{label}</span>
      <strong>{formatLabel(state)} | {fmt(count)}</strong>
      {sub ? <small>{sub}</small> : null}
    </div>
  );
}

function statRows(review: CICDRunCorrelationReview | null): readonly {
  readonly color: string;
  readonly label: string;
  readonly sub: string;
  readonly value: string | number;
}[] {
  const count = review?.count.status === "ready" ? review.count.data : null;
  const list = review?.list.status === "ready" ? review.list.data : null;
  return [
    { color: "var(--teal)", label: "Correlations", sub: "cheap aggregate", value: count?.totalCorrelations ?? "-" },
    { color: "var(--blue)", label: "Rows", sub: list?.truncated ? "truncated" : "bounded list", value: list?.count ?? "-" },
    { color: "var(--ember)", label: "Exact", sub: "outcome rollup", value: count?.byOutcome.exact ?? "-" },
    { color: "var(--violet)", label: "Providers", sub: "reporting buckets", value: count ? Object.keys(count.byProvider).length : "-" }
  ];
}

function formFromSearch(searchParams: URLSearchParams, demoMode = false): FormState {
  return {
    artifactDigest: searchParams.get("artifact_digest") ?? "",
    commitSha: searchParams.get("commit_sha") ?? "",
    environment: searchParams.get("environment") ?? (demoMode ? demoDefaults.cicd.environment : ""),
    imageRef: searchParams.get("image_ref") ?? "",
    limit: searchParams.get("limit") ?? "25",
    outcome: searchParams.get("outcome") ?? "",
    provider: searchParams.get("provider") ?? "",
    providerRunId: searchParams.get("provider_run_id") ?? searchParams.get("run_id") ?? "",
    repositoryId: searchParams.get("repository_id") ?? (demoMode ? demoDefaults.cicd.repositoryId : ""),
    scopeId: searchParams.get("scope_id") ?? ""
  };
}

function addParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) params.set(key, trimmed);
}

function formatLabel(value: string): string {
  return value.replace(/_/g, " ");
}

function shortSha(value: string): string {
  return value.length > 12 ? value.slice(0, 12) : value;
}

function shortDigest(value: string): string {
  if (value.startsWith("sha256:") && value.length > 19) {
    return `${value.slice(0, 19)}...`;
  }
  return value;
}

function classToken(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
}
