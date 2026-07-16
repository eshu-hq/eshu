import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation } from "react-router-dom";

import {
  EMPTY_DRIFT_ERRORS,
  EMPTY_DRIFT_STATE,
  loadCloudDriftSurfaces,
  type DriftState,
  type DriftSurfaceErrors,
} from "./cloudDriftLoad";
import {
  EmptyRow,
  listText,
  ManagementExplanationPanel,
  surfaceErrorMessage,
  TruthPair,
  UnmanagedRow,
} from "./CloudDriftPresentation";
import {
  cleanFilter,
  EMPTY_FILTERS,
  filtersFromSearch,
  hasBoundedScope,
  queryFor,
  shouldLoadAwsSurfaces,
  type DriftFilters,
} from "./CloudDriftQuery";
import { CloudDriftSummary } from "./CloudDriftSummary";
import { CloudDriftToolbar } from "./CloudDriftToolbar";
import type { EshuApiClient } from "../api/client";
import {
  loadCloudRuntimeDriftFindings,
  loadIaCManagementExplanation,
  type CloudDriftExactQuery,
  type IaCManagementExplanation,
  type UnmanagedCloudResourceFinding,
} from "../api/cloudDrift";
import {
  loadCloudRuntimeDriftPacket,
  type InvestigationPacketResult,
} from "../api/investigationPacket";
import { Badge, Panel } from "../components/atoms";
import { InvestigationEvidencePacketReader } from "../components/InvestigationEvidencePacketReader";
import "./liveInventory.css";

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
  const [state, setState] = useState<DriftState>(EMPTY_DRIFT_STATE);
  const [busy, setBusy] = useState(false);
  const [surfaceErrors, setSurfaceErrors] = useState<DriftSurfaceErrors>(EMPTY_DRIFT_ERRORS);
  const [paginationError, setPaginationError] = useState("");
  const [explanation, setExplanation] = useState<IaCManagementExplanation | null>(null);
  const [explainBusyArn, setExplainBusyArn] = useState("");
  const [explainError, setExplainError] = useState("");
  const [packet, setPacket] = useState<InvestigationPacketResult | null>(null);
  const [packetBusy, setPacketBusy] = useState(false);
  const [packetError, setPacketError] = useState("");
  const scopeGeneration = useRef(0);
  const explanationRequest = useRef(0);
  const packetRequest = useRef(0);
  const paginationRequest = useRef(0);
  const hasScope = hasBoundedScope(applied);
  const multiEnabled = Boolean(client && hasScope);
  const awsEnabled = Boolean(client && hasScope && shouldLoadAwsSurfaces(applied));

  const loadAll = useCallback(
    (filters: DriftFilters, offset: number) => {
      const generation = ++scopeGeneration.current;
      explanationRequest.current += 1;
      packetRequest.current += 1;
      paginationRequest.current += 1;
      setExplainBusyArn("");
      setExplainError("");
      setPacketBusy(false);
      setPacketError("");
      setBusy(Boolean(client && hasBoundedScope(filters)));
      setSurfaceErrors(EMPTY_DRIFT_ERRORS);
      setPaginationError("");
      setState(EMPTY_DRIFT_STATE);
      setExplanation(null);
      setPacket(null);
      if (!client || !hasBoundedScope(filters)) return () => undefined;
      let cancelled = false;
      const query = queryFor(filters, offset);
      const awsEnabled = shouldLoadAwsSurfaces(filters);
      void loadCloudDriftSurfaces(client, query, awsEnabled).then((result) => {
        if (!cancelled && scopeGeneration.current === generation) {
          setState(result.state);
          setSurfaceErrors(result.errors);
          setBusy(false);
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
    scopeGeneration.current += 1;
    explanationRequest.current += 1;
    packetRequest.current += 1;
    paginationRequest.current += 1;
    setExplainBusyArn("");
    setExplainError("");
    setPacketBusy(false);
    setPacketError("");
    // Copy even when values are unchanged so a deliberate re-submit always
    // starts a fresh generation instead of only invalidating the current one.
    setApplied({ ...draft });
  }

  function reset(): void {
    scopeGeneration.current += 1;
    explanationRequest.current += 1;
    packetRequest.current += 1;
    paginationRequest.current += 1;
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
    setState(EMPTY_DRIFT_STATE);
    setBusy(false);
    setSurfaceErrors(EMPTY_DRIFT_ERRORS);
    setPaginationError("");
    setExplanation(null);
    setExplainBusyArn("");
    setExplainError("");
    setPacket(null);
    setPacketBusy(false);
    setPacketError("");
  }

  function nextMultiPage(): void {
    if (!client || !state.multi?.nextOffset) return;
    const generation = scopeGeneration.current;
    const request = ++paginationRequest.current;
    setBusy(true);
    setPaginationError("");
    void loadCloudRuntimeDriftFindings(client, queryFor(applied, state.multi.nextOffset))
      .then((multi) => {
        if (scopeGeneration.current !== generation || paginationRequest.current !== request) return;
        setState((current) => ({ ...current, multi }));
        setBusy(false);
      })
      .catch((err: unknown) => {
        if (scopeGeneration.current !== generation || paginationRequest.current !== request) return;
        setBusy(false);
        setPaginationError(err instanceof Error ? err.message : "failed to load next drift page");
      });
  }

  function explainStatus(finding: UnmanagedCloudResourceFinding): void {
    if (!client || finding.arn === "") return;
    const generation = scopeGeneration.current;
    const request = ++explanationRequest.current;
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
        if (scopeGeneration.current !== generation || explanationRequest.current !== request)
          return;
        setExplanation(result);
        setExplainBusyArn("");
      })
      .catch((err: unknown) => {
        if (scopeGeneration.current !== generation || explanationRequest.current !== request)
          return;
        setExplainError(err instanceof Error ? err.message : "failed to explain management status");
        setExplainBusyArn("");
      });
  }

  function loadPacket(): void {
    if (!client || !hasBoundedScope(applied)) return;
    const generation = scopeGeneration.current;
    const request = ++packetRequest.current;
    setPacketBusy(true);
    setPacketError("");
    void loadCloudRuntimeDriftPacket(client, {
      accountId: cleanFilter(applied.accountId),
      maxSourceFacts: 50,
      provider: cleanFilter(applied.provider),
      scopeId: cleanFilter(applied.scopeId),
    })
      .then((result) => {
        if (scopeGeneration.current !== generation || packetRequest.current !== request) return;
        setPacket(result);
        setPacketBusy(false);
      })
      .catch((err: unknown) => {
        if (scopeGeneration.current !== generation || packetRequest.current !== request) return;
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

      <CloudDriftToolbar
        busy={busy}
        draft={draft}
        onDraft={setDraft}
        onReset={reset}
        onSubmit={submit}
      />

      <CloudDriftSummary
        awsEnabled={awsEnabled}
        multiEnabled={multiEnabled}
        state={state}
        surfaceErrors={surfaceErrors}
      />

      {!hasScope ? (
        <p className="empty mt">Enter a scope or account to load drift evidence.</p>
      ) : null}
      {busy ? <p className="empty mt">Loading drift findings...</p> : null}
      {surfaceErrorMessage(surfaceErrors) ? (
        <p className="src-err mt" role="alert">
          Failed to load drift findings: {surfaceErrorMessage(surfaceErrors)}
        </p>
      ) : null}
      {paginationError ? (
        <p className="src-err mt" role="alert">
          Failed to load next multi-cloud drift page: {paginationError}
        </p>
      ) : null}

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
                  <th />
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
