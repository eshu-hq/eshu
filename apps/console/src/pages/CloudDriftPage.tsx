import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import {
  loadAwsRuntimeDriftFindings,
  loadCloudRuntimeDriftFindings,
  loadIaCManagementExplanation,
  loadTerraformImportPlanCandidates,
  loadUnmanagedCloudResources,
  type AwsRuntimeDriftPage,
  type CloudDriftExactQuery,
  type CloudDriftProvider,
  type CloudDriftQuery,
  type CloudRuntimeDriftPage,
  type IaCManagementExplanation,
  type TerraformImportPlanCandidate,
  type TerraformImportPlanPage,
  type UnmanagedCloudResourceFinding,
  type UnmanagedCloudResourcesPage,
} from "../api/cloudDrift";
import {
  loadCloudRuntimeDriftPacket,
  type InvestigationPacketResult,
} from "../api/investigationPacket";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { InvestigationEvidencePacketReader } from "../components/InvestigationEvidencePacketReader";
import { uiFresh, uiTruth } from "../console/types";
import "./liveInventory.css";

const PAGE_LIMIT = 50;

interface DriftFilters {
  readonly accountId: string;
  readonly provider: CloudDriftProvider;
  readonly region: string;
  readonly scopeId: string;
}

const EMPTY_FILTERS: DriftFilters = {
  accountId: "",
  provider: "",
  region: "",
  scopeId: "",
};

interface DriftState {
  readonly aws: AwsRuntimeDriftPage | null;
  readonly importPlan: TerraformImportPlanPage | null;
  readonly multi: CloudRuntimeDriftPage | null;
  readonly unmanaged: UnmanagedCloudResourcesPage | null;
}

const EMPTY_STATE: DriftState = {
  aws: null,
  importPlan: null,
  multi: null,
  unmanaged: null,
};

export function CloudDriftPage({
  client,
  demoDefaults,
}: {
  readonly client?: EshuApiClient;
  readonly demoDefaults?: DriftFilters;
}): React.JSX.Element {
  const location = useLocation();
  const initial = useMemo(
    () => filtersFromSearch(location.search, demoDefaults),
    [location.search, demoDefaults],
  );
  const [draft, setDraft] = useState<DriftFilters>(initial);
  const [applied, setApplied] = useState<DriftFilters>(initial);
  const [state, setState] = useState<DriftState>(EMPTY_STATE);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [explanation, setExplanation] = useState<IaCManagementExplanation | null>(null);
  const [explainBusyArn, setExplainBusyArn] = useState("");
  const [explainError, setExplainError] = useState("");
  const [packet, setPacket] = useState<InvestigationPacketResult | null>(null);
  const [packetBusy, setPacketBusy] = useState(false);
  const [packetError, setPacketError] = useState("");
  const hasScope = hasBoundedScope(applied);

  const loadAll = useCallback(
    (filters: DriftFilters, offset: number) => {
      if (!client || !hasBoundedScope(filters)) return () => undefined;
      let cancelled = false;
      setBusy(true);
      setError("");
      setExplanation(null);
      setPacket(null);
      setPacketError("");
      const query = queryFor(filters, offset);
      const awsEnabled = shouldLoadAwsSurfaces(filters);
      const awsPromise = awsEnabled
        ? loadAwsRuntimeDriftFindings(client, query)
        : Promise.resolve<AwsRuntimeDriftPage | null>(null);
      const unmanagedPromise = awsEnabled
        ? loadUnmanagedCloudResources(client, query)
        : Promise.resolve<UnmanagedCloudResourcesPage | null>(null);
      const importPromise = awsEnabled
        ? loadTerraformImportPlanCandidates(client, query)
        : Promise.resolve<TerraformImportPlanPage | null>(null);
      void Promise.all([
        loadCloudRuntimeDriftFindings(client, query),
        awsPromise,
        unmanagedPromise,
        importPromise,
      ])
        .then(([multi, aws, unmanaged, importPlan]) => {
          if (!cancelled) {
            setState({ aws, importPlan, multi, unmanaged });
            setBusy(false);
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            setState(EMPTY_STATE);
            setBusy(false);
            setError(err instanceof Error ? err.message : "failed to load drift findings");
          }
        });
      return () => {
        cancelled = true;
      };
    },
    [client],
  );

  useEffect(() => loadAll(applied, 0), [loadAll, applied]);

  function submit(): void {
    setApplied(draft);
  }

  function reset(): void {
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
    setState(EMPTY_STATE);
    setError("");
    setExplanation(null);
    setPacket(null);
    setPacketError("");
  }

  function nextMultiPage(): void {
    if (!client || !state.multi?.nextOffset) return;
    setBusy(true);
    setError("");
    void loadCloudRuntimeDriftFindings(client, queryFor(applied, state.multi.nextOffset))
      .then((multi) => {
        setState((current) => ({ ...current, multi }));
        setBusy(false);
      })
      .catch((err: unknown) => {
        setBusy(false);
        setError(err instanceof Error ? err.message : "failed to load drift findings");
      });
  }

  function explainStatus(finding: UnmanagedCloudResourceFinding): void {
    if (!client || finding.arn === "") return;
    setExplainBusyArn(finding.arn);
    setExplainError("");
    const query: CloudDriftExactQuery = {
      accountId: finding.accountId || applied.accountId,
      arn: finding.arn,
      region: finding.region || applied.region,
      scopeId: applied.scopeId,
    };
    void loadIaCManagementExplanation(client, query)
      .then((result) => {
        setExplanation(result);
        setExplainBusyArn("");
      })
      .catch((err: unknown) => {
        setExplainError(err instanceof Error ? err.message : "failed to explain management status");
        setExplainBusyArn("");
      });
  }

  function loadPacket(): void {
    if (!client || !hasBoundedScope(applied)) return;
    setPacketBusy(true);
    setPacketError("");
    void loadCloudRuntimeDriftPacket(client, {
      accountId: cleanFilter(applied.accountId),
      maxSourceFacts: 50,
      provider: cleanFilter(applied.provider),
      scopeId: cleanFilter(applied.scopeId),
    })
      .then((result) => {
        setPacket(result);
        setPacketBusy(false);
      })
      .catch((err: unknown) => {
        setPacket(null);
        setPacketBusy(false);
        setPacketError(err instanceof Error ? err.message : "failed to load drift evidence packet");
      });
  }

  const importByFindingId = useMemo(
    () =>
      new Map(
        (state.importPlan?.candidates ?? []).map((candidate) => [candidate.findingId, candidate]),
      ),
    [state.importPlan],
  );

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Cloud Drift</h2>
        <p>
          Runtime drift, unmanaged cloud resources, and import-plan candidate status from
          reducer-backed read models.
        </p>
      </div>

      <form
        className="evidence-toolbar"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <select
          aria-label="Provider filter"
          className="popover-input"
          value={draft.provider}
          onChange={(event) =>
            setDraft((current) => ({
              ...current,
              provider: event.target.value as CloudDriftProvider,
            }))
          }
        >
          <option value="">Provider</option>
          <option value="aws">AWS</option>
          <option value="gcp">GCP</option>
          <option value="azure">Azure</option>
        </select>
        <input
          aria-label="Account ID filter"
          className="popover-input mono"
          placeholder="account_id"
          value={draft.accountId}
          onChange={(event) =>
            setDraft((current) => ({ ...current, accountId: event.target.value }))
          }
        />
        <input
          aria-label="Region filter"
          className="popover-input mono"
          placeholder="region"
          value={draft.region}
          onChange={(event) => setDraft((current) => ({ ...current, region: event.target.value }))}
        />
        <input
          aria-label="Scope ID filter"
          className="popover-input mono"
          placeholder="scope_id"
          value={draft.scopeId}
          onChange={(event) => setDraft((current) => ({ ...current, scopeId: event.target.value }))}
        />
        <button className="btn-ghost active" disabled={busy} type="submit">
          Load drift findings
        </button>
        <button className="btn-ghost" disabled={busy} type="button" onClick={reset}>
          Reset
        </button>
      </form>

      <div className="grid g-4">
        <StatTile
          label="Multi-cloud drift"
          value={state.multi?.totalFindingsCount ?? 0}
          color="var(--blue)"
          sub={pageSub(state.multi?.truncated)}
        />
        <StatTile
          label="AWS drift"
          value={state.aws?.totalFindingsCount ?? 0}
          color="var(--ember)"
          sub={shouldLoadAwsSurfaces(applied) ? "bounded AWS findings" : "AWS scope required"}
        />
        <StatTile
          label="Unmanaged"
          value={state.unmanaged?.totalFindingsCount ?? 0}
          color="var(--teal)"
          sub="IaC management readback"
        />
        <StatTile
          label="Import candidates"
          value={state.importPlan?.readyCount ?? 0}
          color="var(--violet)"
          sub={`${state.importPlan?.refusedCount ?? 0} refused`}
        />
      </div>

      {!hasScope ? (
        <p className="empty mt">Enter a scope or account to load drift evidence.</p>
      ) : null}
      {busy ? <p className="empty mt">Loading drift findings...</p> : null}
      {error ? <p className="empty mt">Failed to load drift findings: {error}</p> : null}

      <div className="evidence-workbench mt">
        <Panel
          className="flush"
          title="Provider-neutral runtime drift"
          sub={state.multi?.story || "Bounded by canonical scope or provider alias"}
          action={
            state.multi ? (
              <span className="row compact">
                <TruthPair truth={state.multi.truth} />
                <button
                  className="btn-ghost active"
                  disabled={packetBusy || !hasScope}
                  type="button"
                  onClick={loadPacket}
                >
                  {packetBusy ? "Loading packet..." : "Load drift evidence packet"}
                </button>
              </span>
            ) : null
          }
        >
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Resource</th>
                  <th>Provider</th>
                  <th>Finding</th>
                  <th>Source state</th>
                  <th>Evidence</th>
                  <th>Safety</th>
                </tr>
              </thead>
              <tbody>
                {(state.multi?.findings ?? []).map((finding) => (
                  <tr key={finding.id}>
                    <td className="cell-stack">
                      <span className="t-name">{finding.canonicalResourceId || finding.id}</span>
                      <small>{finding.scopeId}</small>
                    </td>
                    <td>
                      {finding.provider ? (
                        <Badge tone="violet">{finding.provider}</Badge>
                      ) : (
                        <span className="t-mut">-</span>
                      )}
                    </td>
                    <td className="cell-stack">
                      <span>{finding.findingKind || "-"}</span>
                      <small>
                        {finding.managementStatus
                          ? `management ${finding.managementStatus}`
                          : "management -"}
                      </small>
                    </td>
                    <td>
                      {finding.sourceState ? (
                        <Badge tone={finding.sourceState === "rejected" ? "crit" : "teal"}>
                          {finding.sourceState}
                        </Badge>
                      ) : (
                        "-"
                      )}
                    </td>
                    <td className="t-mut">{listText(finding.missingEvidence) || "complete"}</td>
                    <td>{finding.safetyOutcome || "read_only"}</td>
                  </tr>
                ))}
                {state.multi && state.multi.findings.length === 0 ? (
                  <EmptyRow
                    cols={6}
                    text="No provider-neutral drift findings matched this scope."
                  />
                ) : null}
              </tbody>
            </table>
          </div>
          {state.multi?.nextOffset ? (
            <div className="pager-row">
              <span className="t-mut">
                More multi-cloud drift available at offset {state.multi.nextOffset}
              </span>
              <button
                className="btn-ghost active"
                disabled={busy}
                type="button"
                onClick={nextMultiPage}
              >
                Next multi-cloud drift page
              </button>
            </div>
          ) : null}
          {packetError ? <p className="src-err">{packetError}</p> : null}
          {packet ? (
            <div className="mt">
              <InvestigationEvidencePacketReader packet={packet.packet} />
            </div>
          ) : null}
        </Panel>

        <Panel
          className="flush"
          title="Unmanaged resources"
          sub={state.unmanaged?.story || "AWS IaC management status and import candidate context"}
          action={state.unmanaged ? <TruthPair truth={state.unmanaged.truth} /> : null}
        >
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Resource</th>
                  <th>Status</th>
                  <th>Missing evidence</th>
                  <th>Import plan</th>
                  <th>Safety</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {(state.unmanaged?.findings ?? []).map((finding) => (
                  <UnmanagedRow
                    candidate={importByFindingId.get(finding.id)}
                    finding={finding}
                    key={finding.id}
                    onExplain={explainStatus}
                    pending={explainBusyArn === finding.arn}
                  />
                ))}
                {state.unmanaged && state.unmanaged.findings.length === 0 ? (
                  <EmptyRow
                    cols={6}
                    text="No unmanaged-resource findings matched this AWS scope."
                  />
                ) : null}
              </tbody>
            </table>
          </div>
        </Panel>

        <Panel
          className="flush"
          title="AWS runtime drift"
          sub={state.aws?.story || "Outcome and promotion posture for AWS reducer findings"}
          action={state.aws ? <TruthPair truth={state.aws.truth} /> : null}
        >
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>ARN</th>
                  <th>Outcome</th>
                  <th>Promotion</th>
                  <th>Finding</th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {(state.aws?.findings ?? []).map((finding) => (
                  <tr key={finding.id}>
                    <td className="t-name">
                      {finding.arn ? `runtime ${finding.arn}` : finding.id}
                    </td>
                    <td>{finding.outcome || "-"}</td>
                    <td>{finding.promotionOutcome || "-"}</td>
                    <td>{finding.findingKind || "-"}</td>
                    <td className="t-mut">{listText(finding.missingEvidence) || "complete"}</td>
                  </tr>
                ))}
                {state.aws && state.aws.findings.length === 0 ? (
                  <EmptyRow cols={5} text="No AWS runtime drift findings matched this scope." />
                ) : null}
              </tbody>
            </table>
          </div>
        </Panel>

        <ManagementExplanationPanel error={explainError} explanation={explanation} />
      </div>
    </div>
  );
}

function UnmanagedRow({
  candidate,
  finding,
  onExplain,
  pending,
}: {
  readonly candidate: TerraformImportPlanCandidate | undefined;
  readonly finding: UnmanagedCloudResourceFinding;
  readonly onExplain: (finding: UnmanagedCloudResourceFinding) => void;
  readonly pending: boolean;
}): React.JSX.Element {
  return (
    <tr>
      <td className="cell-stack">
        <span className="t-name">{finding.arn || finding.resourceId || finding.id}</span>
        <small>
          {finding.provider} {finding.accountId} {finding.region}
        </small>
      </td>
      <td>{finding.managementStatus || "-"}</td>
      <td className="t-mut">{listText(finding.missingEvidence) || "complete"}</td>
      <td className="cell-stack">
        {candidate ? (
          <>
            <span>{candidate.suggestedResourceAddress || candidate.status}</span>
            <small>
              {candidate.status}
              {candidate.refusalReasons.length > 0 ? `: ${listText(candidate.refusalReasons)}` : ""}
            </small>
            <Link to={importContextHref(candidate)}>Open import context</Link>
          </>
        ) : (
          <span className="t-mut">No candidate returned</span>
        )}
      </td>
      <td>{finding.safetyOutcome || "read_only"}</td>
      <td>
        <button
          className="btn-ghost"
          disabled={pending}
          type="button"
          onClick={() => onExplain(finding)}
        >
          {pending ? "Explaining..." : `Explain status for ${finding.arn}`}
        </button>
      </td>
    </tr>
  );
}

function ManagementExplanationPanel({
  error,
  explanation,
}: {
  readonly error: string;
  readonly explanation: IaCManagementExplanation | null;
}): React.JSX.Element {
  return (
    <Panel
      title="Management explanation"
      sub={explanation?.arn || "Exact-resource evidence drilldown"}
    >
      {error ? <p className="empty">Failed to explain management status: {error}</p> : null}
      {explanation ? (
        <div className="evidence-card-list">
          <div className="evidence-card">
            <strong>{explanation.story}</strong>
            <span className="t-mut">Safety gate: {explanation.safetyOutcome || "read_only"}</span>
          </div>
          {explanation.evidenceGroups.map((group) => (
            <div className="evidence-card" key={group.layer}>
              <strong>
                {group.layer || "evidence"} · {group.count}
              </strong>
              {group.evidence.map((item) => (
                <span className="cell-stack mono t-mut" key={item.id}>
                  <span>{item.evidenceType}</span>
                  <small>
                    {item.key} · {item.value}
                  </small>
                </span>
              ))}
            </div>
          ))}
        </div>
      ) : (
        <p className="empty">
          Select an unmanaged resource to inspect its reducer evidence groups.
        </p>
      )}
    </Panel>
  );
}

function TruthPair({
  truth,
}: {
  readonly truth: { readonly freshness: string; readonly level: string };
}): React.JSX.Element {
  return (
    <span className="panel-action-stack">
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness)} />
    </span>
  );
}

function EmptyRow({
  cols,
  text,
}: {
  readonly cols: number;
  readonly text: string;
}): React.JSX.Element {
  return (
    <tr>
      <td className="empty" colSpan={cols}>
        {text}
      </td>
    </tr>
  );
}

function filtersFromSearch(search: string, defaults: DriftFilters | undefined): DriftFilters {
  const params = new URLSearchParams(search);
  const provider = params.get("provider") ?? "";
  return {
    accountId: params.get("account_id") ?? defaults?.accountId ?? "",
    provider:
      provider === "aws" || provider === "gcp" || provider === "azure"
        ? provider
        : (defaults?.provider ?? ""),
    region: params.get("region") ?? defaults?.region ?? "",
    scopeId: params.get("scope_id") ?? defaults?.scopeId ?? "",
  };
}

function queryFor(filters: DriftFilters, offset: number): CloudDriftQuery {
  return {
    accountId: filters.accountId.trim() || undefined,
    limit: PAGE_LIMIT,
    offset,
    provider: filters.provider,
    region: filters.region.trim() || undefined,
    scopeId: filters.scopeId.trim() || undefined,
  };
}

function cleanFilter(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length === 0 ? undefined : trimmed;
}

function hasBoundedScope(filters: DriftFilters): boolean {
  return filters.scopeId.trim() !== "" || filters.accountId.trim() !== "";
}

function shouldLoadAwsSurfaces(filters: DriftFilters): boolean {
  return filters.accountId.trim() !== "" || filters.scopeId.trim().startsWith("aws:");
}

function importContextHref(candidate: TerraformImportPlanCandidate): string {
  const params = new URLSearchParams();
  if (candidate.accountId) params.set("account_id", candidate.accountId);
  if (candidate.region) params.set("region", candidate.region);
  if (candidate.arn) params.set("arn", candidate.arn);
  return `/replatforming?${params.toString()}`;
}

function listText(values: readonly string[]): string {
  return values.filter((value) => value.trim() !== "").join(", ");
}

function pageSub(truncated: boolean | undefined): string {
  return truncated ? "more available" : "bounded page";
}
