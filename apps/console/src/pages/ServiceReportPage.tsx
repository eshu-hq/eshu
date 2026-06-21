import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import {
  loadServiceInvestigation,
  type ServiceInvestigation,
  type ServiceReportResult
} from "../api/serviceInvestigation";
import type { ConsoleModel } from "../console/types";
import { uiFresh, uiTruth } from "../console/types";
import { defaultServiceName } from "../console/defaultEntity";
import { Badge, FreshDot, Panel, TruthChip } from "../components/atoms";
import "./serviceReport.css";

type NextCall = ServiceInvestigation["nextCalls"][number];

// ServiceReportPage is the report-mode view of a service: it renders the
// service intelligence report (coverage, findings, repository scope) and
// suggested investigations, with links into the graph view. Every section is
// driven by the investigation packet; complete, partial, unsupported, empty,
// stale, and API-failure states are all preserved as visible UI.
export function ServiceReportPage({
  client,
  model,
  onOpenService
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
  readonly onOpenService?: (name: string) => void;
}): React.JSX.Element {
  const params = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const routeName = (params.serviceName ?? searchParams.get("service") ?? "").trim();
  const [input, setInput] = useState(routeName);
  const [result, setResult] = useState<ServiceReportResult | null>(null);
  const [busy, setBusy] = useState(false);
  const loadedRef = useRef<string | null>(null);
  const loadTokenRef = useRef(0);

  const runLoad = useCallback(
    async (serviceName: string) => {
      const trimmed = serviceName.trim();
      if (!client || trimmed.length === 0) {
        return;
      }
      const token = (loadTokenRef.current += 1);
      setBusy(true);
      try {
        const loaded = await loadServiceInvestigation(client, trimmed);
        if (token === loadTokenRef.current) {
          setResult(loaded);
        }
      } finally {
        if (token === loadTokenRef.current) {
          setBusy(false);
        }
      }
    },
    [client]
  );

  useEffect(() => {
    // Both /service-report and /service-report/:serviceName render the same page
    // instance, so navigating back to the bare route must clear the prior report
    // rather than leave stale evidence under a route that no longer selects it.
    if (routeName.length === 0) {
      // Auto-load a sensible default on open: when the live catalog has a
      // service, redirect the bare route to it so the report renders evidence
      // immediately instead of an empty form. The form/picker still overrides.
      const fallback = client ? defaultServiceName(model) : "";
      if (fallback.length > 0) {
        navigate(`/service-report/${encodeURIComponent(fallback)}`, { replace: true });
        return;
      }
      loadedRef.current = null;
      loadTokenRef.current += 1;
      setResult(null);
      setInput("");
      return;
    }
    if (routeName === loadedRef.current) {
      return;
    }
    loadedRef.current = routeName;
    setInput(routeName);
    void runLoad(routeName);
  }, [client, model, navigate, routeName, runLoad]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const next = input.trim();
    if (next.length === 0) {
      return;
    }
    if (next === routeName) {
      loadedRef.current = null;
      void runLoad(next);
      return;
    }
    navigate(`/service-report/${encodeURIComponent(next)}`);
  }

  const services = model.services.slice(0, 8);

  return (
    <div className="srp-page">
      <Panel
        title="Service intelligence report"
        sub="Read the bounded service investigation packet with suggested next investigations."
      >
        <form className="srp-form" onSubmit={submit}>
          <label className="srp-field">
            <span>Service name</span>
            <input
              autoComplete="off"
              list="srp-service-options"
              name="serviceName"
              onChange={(event) => setInput(event.target.value)}
              placeholder="Service name"
              value={input}
            />
          </label>
          <datalist id="srp-service-options">
            {model.services.map((service) => <option key={service.id} value={service.name} />)}
          </datalist>
          <button className="btn" disabled={busy || !client} type="submit">
            {busy ? "Loading…" : "Open report"}
          </button>
        </form>
        {services.length > 0 ? (
          <div className="srp-chips" aria-label="Known services">
            {services.map((service) => (
              <button
                key={service.id}
                className="srp-chip"
                onClick={() => navigate(`/service-report/${encodeURIComponent(service.name)}`)}
                type="button"
              >
                {service.name}
              </button>
            ))}
          </div>
        ) : null}
        {!client ? <p className="srp-muted">Connect to an Eshu API to open service reports.</p> : null}
      </Panel>

      {result !== null ? <ServiceReportResultView onOpenService={onOpenService} result={result} /> : null}
    </div>
  );
}

function ServiceReportResultView({
  onOpenService,
  result
}: {
  readonly onOpenService?: (name: string) => void;
  readonly result: ServiceReportResult;
}): React.JSX.Element {
  const { error, investigation, serviceName, storyPath, truth } = result;

  if (error !== null) {
    return (
      <Panel title={serviceName || "Service"} className="srp-result">
        <div className="srp-state srp-error" role="alert">
          <strong>{error.code}: {error.message}</strong>
          <p>No report content is shown because the investigation route did not return a packet.</p>
        </div>
      </Panel>
    );
  }

  const graphHref = serviceName.length > 0 ? `/service-story/${encodeURIComponent(serviceName)}` : undefined;

  return (
    <Panel
      className="srp-result"
      title={serviceName || "Service story"}
      action={(
        <div className="srp-actions">
          {graphHref !== undefined ? <Link className="btn ghost" to={graphHref}>Open evidence graph</Link> : null}
          {onOpenService !== undefined && serviceName.length > 0 ? (
            <button className="btn ghost" onClick={() => onOpenService(serviceName)} type="button">Open service</button>
          ) : null}
        </div>
      )}
    >
      <div className="srp-truth">
        {truth === null ? (
          <Badge tone="warn">truth unavailable</Badge>
        ) : (
          <>
            {truth.capability ? <span className="mono">{truth.capability}</span> : null}
            <TruthChip level={uiTruth(truth.level)} />
            <FreshDot state={uiFresh(truth.freshness.state)} />
          </>
        )}
      </div>

      <Coverage investigation={investigation} />

      {hasReportEvidence(investigation) ? (
        <div className="srp-grid">
          <EvidenceFamilies families={investigation.evidenceFamilies} />
          <Findings findings={investigation.findings} />
          <Repositories repositories={investigation.repositories} />
          <SuggestedInvestigations calls={investigation.nextCalls} serviceName={serviceName} />
        </div>
      ) : (
        <div className="srp-state" role="status">
          <strong>No investigation evidence for this service.</strong>
          <p>The report route returned a packet with no findings, families, or scope to show.</p>
        </div>
      )}

      {storyPath.length > 0 ? (
        <p className="srp-muted srp-paths">Source: <span className="mono">{storyPath}</span></p>
      ) : null}
    </Panel>
  );
}

function Coverage({ investigation }: { readonly investigation: ServiceInvestigation }): React.JSX.Element {
  const { coverage } = investigation;
  return (
    <section className="srp-coverage" aria-label="Coverage">
      <div className="srp-coverage-head">
        <Badge tone={coverageTone(coverage.state)}>{coverageLabel(coverage.state)}</Badge>
        {coverage.truncated ? <Badge tone="warn">truncated</Badge> : null}
      </div>
      <p>{coverage.reason}</p>
      <p className="srp-muted">
        {coverage.repositoriesWithEvidence} of {coverage.repositoryCount} repositories carried evidence.
      </p>
    </section>
  );
}

function EvidenceFamilies({ families }: { readonly families: readonly string[] }): React.JSX.Element {
  return (
    <section className="srp-section" aria-label="Evidence families">
      <h3>What Eshu checked</h3>
      {families.length === 0 ? (
        <p className="srp-muted">No evidence families reported.</p>
      ) : (
        <div className="srp-chip-row">{families.map((family) => <span key={family}>{humanLabel(family)}</span>)}</div>
      )}
    </section>
  );
}

function Findings({ findings }: { readonly findings: ServiceInvestigation["findings"] }): React.JSX.Element {
  return (
    <section className="srp-section" aria-label="Findings">
      <h3>What it found</h3>
      {findings.length === 0 ? (
        <p className="srp-muted">No findings returned.</p>
      ) : (
        <div className="srp-list">
          {findings.map((finding) => (
            <article key={`${finding.family}:${finding.path}:${finding.summary}`}>
              <strong>{humanLabel(finding.family)}</strong>
              <p>{finding.summary}</p>
              {finding.path.length > 0 ? <small className="mono">{finding.path}</small> : null}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function Repositories({ repositories }: { readonly repositories: ServiceInvestigation["repositories"] }): React.JSX.Element {
  return (
    <section className="srp-section" aria-label="Repositories in scope">
      <h3>Repos in scope</h3>
      {repositories.length === 0 ? (
        <p className="srp-muted">No repositories in scope.</p>
      ) : (
        <div className="srp-list">
          {repositories.map((repository) => (
            <article key={repository.name}>
              <strong>{repository.name}</strong>
              <p>{humanList(repository.roles) || "Evidence repository"}</p>
              {repository.evidenceFamilies.length > 0 ? <small>{humanList(repository.evidenceFamilies)}</small> : null}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function SuggestedInvestigations({
  calls,
  serviceName
}: {
  readonly calls: ServiceInvestigation["nextCalls"];
  readonly serviceName: string;
}): React.JSX.Element {
  const deduped = dedupeCalls(calls);
  return (
    <section className="srp-section" data-testid="suggested-investigations" aria-label="Suggested investigations">
      <h3>Suggested investigations</h3>
      {deduped.length === 0 ? (
        <p className="srp-muted">No suggested investigations returned.</p>
      ) : (
        <div className="srp-list">
          {deduped.map((call) => {
            const href = investigationDestination(call, serviceName);
            return (
              <article key={`${call.tool}:${JSON.stringify(call.arguments)}`}>
                {href !== null ? (
                  <Link className="link-btn" to={href}>{call.reason}</Link>
                ) : (
                  <strong>{call.reason}</strong>
                )}
                <p>{humanToolLabel(call.tool)}</p>
                <small className="mono">{argumentSummary(call.arguments)}</small>
                {href === null ? <small className="srp-muted">No console destination for this tool yet.</small> : null}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

// investigationDestination maps a recommended next call onto a console route,
// returning null when no destination is known or the required input is missing —
// so the UI only offers a clickable investigation when its backing inputs are
// valid. The investigate route emits the service identity as `workload_id`
// (service investigation packet next-calls); `service_name` is a fallback.
function investigationDestination(call: NextCall, serviceName: string): string | null {
  const name = firstString(call.arguments.workload_id, call.arguments.service_name, serviceName);
  if (name.length === 0) {
    return null;
  }
  if (call.tool === "get_service_story") {
    return `/service-story/${encodeURIComponent(name)}`;
  }
  return null;
}

function firstString(...values: readonly unknown[]): string {
  for (const value of values) {
    if (typeof value === "string" && value.trim().length > 0) {
      return value.trim();
    }
  }
  return "";
}

function hasReportEvidence(investigation: ServiceInvestigation): boolean {
  return investigation.coverage.repositoryCount > 0 ||
    investigation.evidenceFamilies.length > 0 ||
    investigation.findings.length > 0 ||
    investigation.nextCalls.length > 0 ||
    investigation.repositories.length > 0;
}

function dedupeCalls(calls: ServiceInvestigation["nextCalls"]): ServiceInvestigation["nextCalls"] {
  const seen = new Set<string>();
  return calls.filter((call) => {
    const key = `${call.tool}:${JSON.stringify(call.arguments)}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function argumentSummary(argumentsValue: Record<string, unknown>): string {
  const summary = Object.entries(argumentsValue).map(([key, value]) => `${key}: ${String(value)}`).join(", ");
  return summary.length > 0 ? summary : "No extra arguments";
}

function coverageTone(state: string): "teal" | "warn" | "neutral" {
  const normalized = state.trim().toLowerCase();
  if (normalized === "complete") {
    return "teal";
  }
  if (normalized === "partial") {
    return "warn";
  }
  return "neutral";
}

function coverageLabel(state: string): string {
  const normalized = state.trim().toLowerCase();
  if (normalized === "complete") {
    return "Complete";
  }
  if (normalized === "partial") {
    return "Partial";
  }
  return "Unknown";
}

function humanToolLabel(tool: string): string {
  const labels: Record<string, string> = {
    get_code_relationship_story: "Code relationship story",
    get_relationship_evidence: "Relationship evidence",
    get_service_context: "Service context",
    get_service_story: "Service story",
    investigate_service: "Service investigation",
    trace_deployment_chain: "Deployment chain"
  };
  return labels[tool] ?? humanLabel(tool);
}

function humanList(values: readonly string[]): string {
  return values.map(humanLabel).filter((value) => value.length > 0).join(", ");
}

function humanLabel(value: string): string {
  return value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
    .replace(/\bApi\b/g, "API")
    .replace(/\bMcp\b/g, "MCP")
    .replace(/\bEcs\b/g, "ECS")
    .replace(/\bEks\b/g, "EKS");
}
