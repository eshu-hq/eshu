import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import {
  AmbiguousEvidence,
  EvidencePath,
  IncidentSummary,
  MissingEvidence,
  RelatedChanges,
  Timeline,
  statRows
} from "./IncidentContextSections";
import type { EshuApiClient } from "../api/client";
import {
  loadIncidentContext,
  type IncidentContextLoadResult
} from "../api/incidentContext";
import { Badge, Panel, StatTile } from "../components/atoms";
import type { ConsoleModel } from "../console/types";
import "./incidentContextPage.css";

interface IncidentFormState {
  readonly incidentId: string;
  readonly limit: string;
  readonly provider: string;
  readonly scopeId: string;
  readonly serviceId: string;
  readonly since: string;
  readonly until: string;
}

export function IncidentContextPage({
  client,
  model,
  onOpenService
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
  readonly onOpenService?: (name: string) => void;
}): React.JSX.Element {
  const routeParams = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [form, setForm] = useState<IncidentFormState>(() =>
    formFromSearch(searchParams, routeParams.incidentId)
  );
  const [result, setResult] = useState<IncidentContextLoadResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canLoad = model.source === "live" && client !== undefined;

  const runReview = useCallback(
    async (next: IncidentFormState) => {
      const incidentId = next.incidentId.trim();
      if (!client || incidentId.length === 0) {
        return;
      }
      setBusy(true);
      setError("");
      try {
        const loaded = await loadIncidentContext(client, {
          incidentId,
          limit: Number(next.limit),
          provider: next.provider,
          scopeId: next.scopeId,
          serviceId: next.serviceId,
          since: next.since,
          until: next.until
        });
        setResult(loaded);
      } finally {
        setBusy(false);
      }
    },
    [client]
  );

  useEffect(() => {
    const next = formFromSearch(searchParams, routeParams.incidentId);
    setForm(next);
    if (canLoad && next.incidentId.trim().length > 0) {
      void runReview(next);
    }
  }, [canLoad, routeParams.incidentId, runReview, searchParams]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const incidentId = form.incidentId.trim();
    if (incidentId.length === 0) {
      setError("Incident id is required.");
      return;
    }
    const params = new URLSearchParams({
      provider: form.provider.trim() || "pagerduty"
    });
    addParam(params, "scope_id", form.scopeId);
    addParam(params, "service_id", form.serviceId);
    addParam(params, "since", form.since);
    addParam(params, "until", form.until);
    if (form.limit.trim().length > 0 && form.limit.trim() !== "25") {
      params.set("limit", form.limit.trim());
    }
    navigate({
      pathname: `/incidents/${encodeURIComponent(incidentId)}/context`,
      search: params.toString()
    });
  }

  const context = result?.status === "ready" ? result.context : null;
  const stats = useMemo(() => statRows(context), [context]);

  return (
    <div className="page incident-context-page" style={{ maxWidth: "none" }}>
      <div className="page-intro incident-intro">
        <h2>Incident context</h2>
        <Badge tone={canLoad ? "teal" : "warn"}>{canLoad ? "live API" : "connect live API"}</Badge>
      </div>

      <form className="incident-query" onSubmit={submit}>
        <label className="incident-query-id">
          <span>Incident id</span>
          <input
            aria-label="Incident id"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, incidentId: event.target.value }))}
            placeholder="PABC123"
            value={form.incidentId}
          />
        </label>
        <label>
          <span>Provider</span>
          <input
            aria-label="Provider"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, provider: event.target.value }))}
            value={form.provider}
          />
        </label>
        <label>
          <span>Scope id</span>
          <input
            aria-label="Scope id"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, scopeId: event.target.value }))}
            placeholder="optional"
            value={form.scopeId}
          />
        </label>
        <label>
          <span>Service id</span>
          <input
            aria-label="Service id"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, serviceId: event.target.value }))}
            placeholder="optional"
            value={form.serviceId}
          />
        </label>
        <label>
          <span>Since</span>
          <input
            aria-label="Since"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, since: event.target.value }))}
            placeholder="RFC3339"
            value={form.since}
          />
        </label>
        <label>
          <span>Until</span>
          <input
            aria-label="Until"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, until: event.target.value }))}
            placeholder="RFC3339"
            value={form.until}
          />
        </label>
        <label>
          <span>Limit</span>
          <input
            aria-label="Limit"
            className="popover-input mono"
            inputMode="numeric"
            onChange={(event) => setForm((current) => ({ ...current, limit: event.target.value }))}
            value={form.limit}
          />
        </label>
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Loading..." : "Review incident"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {error ? <p className="src-err">{error}</p> : null}
      {result?.status === "unavailable" ? <p className="src-err">{result.error}</p> : null}

      <div className="grid g-4 mt">
        {stats.map((stat) => (
          <StatTile color={stat.color} key={stat.label} label={stat.label} sub={stat.sub} value={stat.value} />
        ))}
      </div>

      <div className="incident-layout mt">
        <Panel title="Incident">
          {busy ? <p className="empty">Loading incident context...</p> : null}
          {!busy && context === null ? <p className="empty">No incident context loaded.</p> : null}
          {context !== null ? (
            <IncidentSummary
              incident={context.incident}
              onOpenService={onOpenService}
              truth={result?.status === "ready" ? result.truth : null}
            />
          ) : null}
        </Panel>
        <Panel
          sub={context?.answerMetadata.coverage.queryShape ?? "incident_context_evidence_path"}
          title="Coverage"
        >
          {context !== null ? (
            <div className="incident-coverage">
              <span>{context.query.provider}</span>
              <span>{context.query.scopeId || "unscoped"}</span>
              <span>limit {context.query.limit}</span>
              <span>{context.truncated ? "truncated" : "complete"}</span>
              {context.answerMetadata.partialReasons.map((reason) => (
                <span key={reason}>{reason.replace(/_/g, " ")}</span>
              ))}
            </div>
          ) : <p className="empty">No coverage metadata loaded.</p>}
        </Panel>
      </div>

      <div className="incident-evidence-grid mt">
        <Panel title="Evidence path">
          {context !== null ? <EvidencePath rows={context.evidencePath} /> : <p className="empty">No evidence path loaded.</p>}
        </Panel>
        <Panel title="Missing evidence">
          {context !== null ? <MissingEvidence rows={context.missingEvidence} /> : <p className="empty">No missing evidence loaded.</p>}
        </Panel>
        <Panel title="Ambiguity">
          {context !== null ? <AmbiguousEvidence rows={context.ambiguousEvidence} /> : <p className="empty">No ambiguous evidence loaded.</p>}
        </Panel>
      </div>

      <div className="incident-lower-grid mt">
        <Panel title="Related changes">
          {context !== null ? <RelatedChanges rows={context.relatedChanges} /> : <p className="empty">No related changes loaded.</p>}
        </Panel>
        <Panel title="Timeline">
          {context !== null ? <Timeline rows={context.timeline} /> : <p className="empty">No timeline loaded.</p>}
        </Panel>
      </div>
    </div>
  );
}

function formFromSearch(
  searchParams: URLSearchParams,
  routeIncidentId: string | undefined
): IncidentFormState {
  return {
    incidentId:
      routeIncidentId ??
      searchParams.get("incidentId") ??
      searchParams.get("incident_id") ??
      "",
    limit: searchParams.get("limit") ?? "25",
    provider: searchParams.get("provider") ?? "pagerduty",
    scopeId: searchParams.get("scopeId") ?? searchParams.get("scope_id") ?? "",
    serviceId: searchParams.get("serviceId") ?? searchParams.get("service_id") ?? "",
    since: searchParams.get("since") ?? "",
    until: searchParams.get("until") ?? ""
  };
}

function addParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) {
    params.set(key, trimmed);
  }
}
