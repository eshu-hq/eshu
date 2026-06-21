import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import type { EshuTruth } from "../api/envelope";
import {
  loadSecretsIamPosture,
  type SecretsIamBucketCount,
  type SecretsIamInput,
  type SecretsIamPostureGaps,
  type SecretsIamPostureSummary,
  type SecretsIamPrivilegeObservations,
  type SecretsIamReview,
  type SecretsIamSecretAccessPaths,
  type SecretsIamSection,
  type SecretsIamSkippedSection,
  type SecretsIamTrustChains
} from "../api/secretsIam";
import type { ConsoleModel } from "../console/types";
import { fmt, uiFresh, uiTruth } from "../console/types";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import "./secretsIamPage.css";

interface FormState {
  readonly limit: string;
  readonly scopeId: string;
  readonly state: string;
}

const states = ["", "exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"] as const;

export function SecretsIamPage({
  client,
  model
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [form, setForm] = useState<FormState>(() => formFromSearch(searchParams));
  const [review, setReview] = useState<SecretsIamReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canLoad = model.source === "live" && client !== undefined;

  const runReview = useCallback(
    async (next: FormState) => {
      if (!client) return;
      setBusy(true);
      setError("");
      try {
        setReview(await loadSecretsIamPosture(client, inputFromForm(next)));
      } catch (loadError) {
        setReview(null);
        setError(loadError instanceof Error ? loadError.message : "failed to load secrets/IAM posture");
      } finally {
        setBusy(false);
      }
    },
    [client]
  );

  useEffect(() => {
    const next = formFromSearch(searchParams);
    setForm(next);
    if (canLoad) void runReview(next);
  }, [canLoad, runReview, searchParams]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const params = new URLSearchParams();
    addParam(params, "scope_id", form.scopeId);
    addParam(params, "state", form.state);
    if (form.limit.trim().length > 0 && form.limit.trim() !== "25") params.set("limit", form.limit.trim());
    setSearchParams(params);
  }

  const summary = readyData(review?.summary);
  const trustChains = readyData(review?.trustChains);
  const privilegeObservations = readyData(review?.privilegeObservations);
  const secretAccessPaths = readyData(review?.secretAccessPaths);
  const postureGaps = readyData(review?.postureGaps);
  const stats = useMemo(
    () => statRows(trustChains, privilegeObservations, secretAccessPaths, postureGaps),
    [postureGaps, privilegeObservations, secretAccessPaths, trustChains]
  );
  const skippedReason = allSkipped(review) ? review.summary.reason : "";

  return (
    <div className="page secrets-iam-page" style={{ maxWidth: "none" }}>
      <div className="page-intro secrets-iam-intro">
        <h2>Secrets/IAM posture</h2>
        <Badge tone="teal">read model</Badge>
      </div>

      <form className="secrets-iam-query" onSubmit={submit}>
        <FilterInput label="Scope id" value={form.scopeId} onChange={(value) => setForm((current) => ({ ...current, scopeId: value }))} />
        <label>
          <span>State</span>
          <select
            aria-label="State"
            className="popover-input"
            value={form.state}
            onChange={(event) => setForm((current) => ({ ...current, state: event.target.value }))}
          >
            {states.map((state) => <option key={state || "all"} value={state}>{state ? formatLabel(state) : "All states"}</option>)}
          </select>
        </label>
        <FilterInput label="Limit" value={form.limit} onChange={(value) => setForm((current) => ({ ...current, limit: value }))} />
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Loading..." : "Load posture"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {skippedReason ? <p className="inline-state">{skippedReason}</p> : null}
      {error ? <p className="src-err">{error}</p> : null}

      <div className="secrets-iam-boundary mt">
        <strong>Graph projection gated</strong>
        <span>Reducer read models are live independently of the default-off secrets/IAM graph projection.</span>
      </div>

      <div className="grid g-4 mt">
        {stats.map((stat) => (
          <StatTile color={stat.color} key={stat.label} label={stat.label} sub={stat.sub} value={stat.value} />
        ))}
      </div>

      <div className="mt">
        <Panel title="Posture summary" sub="Grouped counts for one reducer scope">
          <SectionStatus section={review?.summary ?? null} />
          {summary ? <SummarySection summary={summary} /> : <EmptyState review={review} section="summary" />}
        </Panel>
      </div>

      <div className="secrets-iam-grid mt">
        <Panel title="Identity trust chains" sub="Trust state, join keys, and missing evidence">
          <SectionStatus section={review?.trustChains ?? null} />
          {trustChains ? <TrustChainsSection chains={trustChains} /> : <EmptyState review={review} section="trustChains" />}
        </Panel>
        <Panel title="Secret access paths" sub="Vault policy-to-KV metadata fingerprints">
          <SectionStatus section={review?.secretAccessPaths ?? null} />
          {secretAccessPaths ? <SecretAccessPathsSection paths={secretAccessPaths} /> : <EmptyState review={review} section="secretAccessPaths" />}
        </Panel>
      </div>

      <div className="secrets-iam-grid mt">
        <Panel title="Privilege posture" sub="Broad or partial posture observations">
          <SectionStatus section={review?.privilegeObservations ?? null} />
          {privilegeObservations ? (
            <PrivilegeObservationsSection observations={privilegeObservations} />
          ) : (
            <EmptyState review={review} section="privilegeObservations" />
          )}
        </Panel>
        <Panel title="Posture gaps" sub="Missing, stale, hidden, and unsupported evidence">
          <SectionStatus section={review?.postureGaps ?? null} />
          {postureGaps ? <PostureGapsSection gaps={postureGaps} /> : <EmptyState review={review} section="postureGaps" />}
        </Panel>
      </div>
    </div>
  );
}

function FilterInput({
  label,
  onChange,
  value
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}): React.JSX.Element {
  return (
    <label>
      <span>{label}</span>
      <input aria-label={label} className="popover-input mono" onChange={(event) => onChange(event.target.value)} placeholder="required" value={value} />
    </label>
  );
}

function SectionStatus<TData>({
  section
}: {
  readonly section: SecretsIamSection<TData> | SecretsIamSkippedSection | null;
}): React.JSX.Element | null {
  if (section === null || section.status === "skipped") return null;
  if (section.status === "unavailable") return <p className="src-err">{section.error}</p>;
  return <TruthSummary truth={section.truth} />;
}

function TruthSummary({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  if (truth === null) return <span className="t-mut">truth envelope unavailable</span>;
  return (
    <span className="secrets-iam-truth">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </span>
  );
}

function SummarySection({ summary }: { readonly summary: SecretsIamPostureSummary }): React.JSX.Element {
  return (
    <div className="secrets-iam-summary">
      <BucketGroup buckets={summary.identityTrustChainsByState} title="Trust chains by state" />
      <BucketGroup buckets={summary.secretAccessPathsByState} title="Access paths by state" />
      <BucketGroup buckets={summary.privilegeObservationsByRiskType} title="Privilege risk types" />
      <BucketGroup buckets={summary.privilegeObservationsBySeverity} title="Privilege severity" />
      <BucketGroup buckets={summary.postureGapsByGapType} title="Posture gap types" />
    </div>
  );
}

function BucketGroup({ buckets, title }: { readonly buckets: readonly SecretsIamBucketCount[]; readonly title: string }): React.JSX.Element {
  return (
    <div className="secrets-iam-buckets">
      <strong>{title}</strong>
      {buckets.map((bucket) => (
        <span key={`${title}:${bucket.bucket}`}>{formatLabel(bucket.bucket)} <b>{fmt(bucket.count)}</b></span>
      ))}
      {buckets.length === 0 ? <span>No bucket counts returned.</span> : null}
    </div>
  );
}

function TrustChainsSection({ chains }: { readonly chains: SecretsIamTrustChains }): React.JSX.Element {
  return (
    <div className="secrets-iam-section">
      <PagingNote cursor={chains.nextCursor?.afterChainId} kind="chain" limit={chains.limit} truncated={chains.truncated} />
      <div className="secrets-iam-cards">
        {chains.chains.map((chain) => (
          <div className="secrets-iam-row-card secrets-iam-chain-card" key={chain.chainId}>
            <CellStack title={chain.chainId} sub={chain.workloadObjectId || chain.workloadKind} />
            <StatusPill value={chain.state} sub={chain.confidence} />
            <CellStack title={chain.serviceAccountJoinKey || chain.iamRoleFingerprint} sub={chain.vaultMountJoinKey} />
            <TokenList values={chain.missingEvidence.length ? chain.missingEvidence : chain.vaultPolicyJoinKeys} />
          </div>
        ))}
        {chains.chains.length === 0 ? <p className="empty">No identity trust chains returned.</p> : null}
      </div>
    </div>
  );
}

function SecretAccessPathsSection({ paths }: { readonly paths: SecretsIamSecretAccessPaths }): React.JSX.Element {
  return (
    <div className="secrets-iam-section">
      <PagingNote cursor={paths.nextCursor?.afterPathId} kind="path" limit={paths.limit} truncated={paths.truncated} />
      <div className="secrets-iam-cards">
        {paths.paths.map((path) => (
          <div className="secrets-iam-row-card" key={path.pathId}>
            <CellStack title={path.pathId} sub={path.kvPathFingerprint} />
            <StatusPill value={path.state} sub={path.confidence} />
            <TokenList values={path.capabilities} />
            <small className="mono">{path.chainId || path.vaultPolicyJoinKey}</small>
          </div>
        ))}
        {paths.paths.length === 0 ? <p className="empty">No secret access paths returned.</p> : null}
      </div>
    </div>
  );
}

function PrivilegeObservationsSection({ observations }: { readonly observations: SecretsIamPrivilegeObservations }): React.JSX.Element {
  return (
    <div className="secrets-iam-section">
      <PagingNote cursor={observations.nextCursor?.afterObservationId} kind="observation" limit={observations.limit} truncated={observations.truncated} />
      <div className="secrets-iam-cards">
        {observations.observations.map((row) => (
          <div className="secrets-iam-row-card" key={row.observationId}>
            <CellStack title={row.riskType || row.observationId} sub={row.subjectFingerprint} />
            <StatusPill value={row.severity || row.state} sub={row.state} />
            <p>{row.reason || "No reason returned."}</p>
          </div>
        ))}
        {observations.observations.length === 0 ? <p className="empty">No privilege posture observations returned.</p> : null}
      </div>
    </div>
  );
}

function PostureGapsSection({ gaps }: { readonly gaps: SecretsIamPostureGaps }): React.JSX.Element {
  return (
    <div className="secrets-iam-section">
      <PagingNote cursor={gaps.nextCursor?.afterGapId} kind="gap" limit={gaps.limit} truncated={gaps.truncated} />
      <div className="secrets-iam-cards">
        {gaps.gaps.map((gap) => (
          <div className="secrets-iam-row-card" key={gap.gapId}>
            <CellStack title={gap.gapType || gap.gapId} sub={gap.serviceAccountJoinKey} />
            <StatusPill value={gap.state} sub={gap.gapId} />
            <p>{gap.reason || "No reason returned."}</p>
            <TokenList values={gap.missingEvidence.length ? gap.missingEvidence : gap.unsupportedLayers} />
          </div>
        ))}
        {gaps.gaps.length === 0 ? <p className="empty">No posture gaps returned.</p> : null}
      </div>
    </div>
  );
}

function PagingNote({
  cursor,
  kind,
  limit,
  truncated
}: {
  readonly cursor?: string;
  readonly kind: string;
  readonly limit: number;
  readonly truncated: boolean;
}): React.JSX.Element {
  const next = truncated && cursor ? ` Next ${kind} cursor ${cursor}.` : truncated ? " More rows are available." : "";
  return <p className="secrets-iam-page-note">Showing up to {fmt(limit)} rows.{next}</p>;
}

function EmptyState({
  review,
  section
}: {
  readonly review: SecretsIamReview | null;
  readonly section: keyof Omit<SecretsIamReview, "input">;
}): React.JSX.Element | null {
  const current = review?.[section];
  if (current?.status === "unavailable") return null;
  if (current?.status === "skipped") return <p className="empty">No secrets/IAM posture data loaded.</p>;
  return <p className="empty">No secrets/IAM posture data loaded.</p>;
}

function CellStack({ sub, title }: { readonly sub: string; readonly title: string }): React.JSX.Element {
  return (
    <span className="cell-stack">
      <span className="t-name">{title || "-"}</span>
      {sub ? <small className="mono">{shortValue(sub)}</small> : null}
    </span>
  );
}

function StatusPill({ sub, value }: { readonly sub?: string; readonly value: string }): React.JSX.Element {
  return (
    <span className={`secrets-iam-state secrets-iam-state-${classToken(value || "unknown")}`}>
      {formatLabel(value || "unknown")}
      {sub ? <small>{formatLabel(sub)}</small> : null}
    </span>
  );
}

function TokenList({ values }: { readonly values: readonly string[] }): React.JSX.Element {
  if (values.length === 0) return <span className="t-mut">none</span>;
  return (
    <span className="secrets-iam-token-list">
      {values.slice(0, 4).map((value) => <span key={value}>{formatLabel(value)}</span>)}
      {values.length > 4 ? <span>+{fmt(values.length - 4)}</span> : null}
    </span>
  );
}

function allSkipped(review: SecretsIamReview | null): review is SecretsIamReview & {
  readonly postureGaps: SecretsIamSkippedSection;
  readonly privilegeObservations: SecretsIamSkippedSection;
  readonly secretAccessPaths: SecretsIamSkippedSection;
  readonly summary: SecretsIamSkippedSection;
  readonly trustChains: SecretsIamSkippedSection;
} {
  return review?.summary.status === "skipped" && review.trustChains.status === "skipped";
}

function readyData<TData>(section: SecretsIamSection<TData> | SecretsIamSkippedSection | undefined): TData | null {
  return section?.status === "ready" ? section.data : null;
}

function statRows(
  trustChains: SecretsIamTrustChains | null,
  privilegeObservations: SecretsIamPrivilegeObservations | null,
  secretAccessPaths: SecretsIamSecretAccessPaths | null,
  postureGaps: SecretsIamPostureGaps | null
): readonly { readonly color: string; readonly label: string; readonly sub: string; readonly value: number | string }[] {
  return [
    { color: "var(--teal)", label: "Trust chains", sub: "bounded scope", value: trustChains?.count ?? "-" },
    { color: "var(--blue)", label: "Access paths", sub: "fingerprints only", value: secretAccessPaths?.count ?? "-" },
    { color: "var(--warn)", label: "Privilege observations", sub: "risk posture", value: privilegeObservations?.count ?? "-" },
    { color: "var(--crit)", label: "Posture gaps", sub: "evidence blockers", value: postureGaps?.count ?? "-" }
  ];
}

function formFromSearch(params: URLSearchParams): FormState {
  return {
    limit: params.get("limit") ?? "25",
    scopeId: params.get("scope_id") ?? "",
    state: params.get("state") ?? ""
  };
}

function inputFromForm(form: FormState): SecretsIamInput {
  return {
    limit: optionalNumber(form.limit),
    scopeId: form.scopeId,
    state: form.state
  };
}

function optionalNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed.length === 0) return undefined;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function addParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) params.set(key, trimmed);
}

function formatLabel(value: string): string {
  return value.replace(/_/g, " ");
}

function classToken(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
}

function shortValue(value: string): string {
  if (value.length <= 58) return value;
  return `${value.slice(0, 55)}...`;
}
