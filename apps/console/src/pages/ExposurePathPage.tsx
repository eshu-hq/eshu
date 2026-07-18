// pages/ExposurePathPage.tsx
// Entrypoint-first Exposure Path surface (#3403). An operator picks an
// internet-facing service entrypoint and the view renders the proven ingress
// chain (Internet -> entrypoint -> edge/runtime) as clickable hops, each opening
// its evidence + truth level inline, with WAF/TLS/hop posture tiles. The chain
// and posture come from the bounded service context at
// GET /api/v0/services/{name}/context (entrypoints, network_paths,
// ingress_posture). The original handler-trace form is kept as advanced mode.
//
// It never fabricates a chain: the synthetic "Internet" origin hop is drawn only
// for an observed-public entrypoint, and WAF/TLS tiles render the honest
// three-valued posture (protected/unprotected/unproven) returned by the backend.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { ExposurePathAdvanced } from "./ExposurePathAdvanced";
import { ExposureServiceSelector } from "./ExposureServiceSelector";
import type { EshuApiClient } from "../api/client";
import {
  loadExposureIngress,
  type ExposureIngress,
  type IngressChain,
  type IngressHop,
  type IngressPostureState,
  type IngressTruth,
} from "../api/exposureIngress";
import {
  exposureServiceOptions,
  resolveExposureServiceSelection,
  type ExposureServiceSelectionResult,
} from "../api/exposureServiceSelection";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { uiFresh, uiTruth, type ServiceRow } from "../console/types";
import "./exposurePathPage.css";

type BadgeTone = "neutral" | "teal" | "ember" | "crit" | "warn" | "violet";

const TRUTH_TONE: Record<IngressTruth, BadgeTone> = {
  observed: "teal",
  derived: "violet",
  unresolved: "neutral",
};

const POSTURE_TONE: Record<IngressPostureState, BadgeTone> = {
  protected: "teal",
  terminated: "teal",
  unprotected: "ember",
  not_terminated: "ember",
  unproven: "neutral",
};

const POSTURE_LABEL: Record<IngressPostureState, string> = {
  protected: "Yes",
  terminated: "Yes",
  unprotected: "No",
  not_terminated: "No",
  unproven: "Unproven",
};

export function ExposurePathPage({
  client,
  services = [],
}: {
  readonly client?: EshuApiClient;
  readonly services?: readonly ServiceRow[];
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [service, setService] = useState(() => searchParams.get("service") ?? "");
  const [ingress, setIngress] = useState<ExposureIngress | null>(null);
  const [selectedChain, setSelectedChain] = useState(0);
  const [selectedHop, setSelectedHop] = useState<IngressHop | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const deepLinkRef = useRef("");
  const requestVersionRef = useRef(0);
  const canLoad = client !== undefined;
  const options = useMemo(() => exposureServiceOptions(services), [services]);

  const clearDeepLink = useCallback(() => {
    deepLinkRef.current = "";
    if (!searchParams.has("service")) {
      return;
    }
    const params = new URLSearchParams(searchParams);
    params.delete("service");
    setSearchParams(params, { replace: true });
  }, [searchParams, setSearchParams]);

  const invalidateActiveRequest = useCallback(() => {
    requestVersionRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
    setBusy(false);
    setError("");
    setIngress(null);
    setSelectedChain(0);
    setSelectedHop(null);
  }, []);

  const runSelection = useCallback(
    async (query: string) => {
      const trimmed = query.trim();
      if (!client || trimmed.length === 0) {
        return;
      }
      const version = requestVersionRef.current + 1;
      requestVersionRef.current = version;
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      setBusy(true);
      setError("");
      setIngress(null);
      setSelectedChain(0);
      setSelectedHop(null);

      const resolution = await resolveExposureServiceSelection({
        client,
        options,
        query: trimmed,
      });
      if (version !== requestVersionRef.current) {
        return;
      }
      if (resolution.status !== "resolved") {
        setError(selectionError(resolution));
        abortRef.current = null;
        setBusy(false);
        return;
      }

      const canonicalID = resolution.option.canonicalId;
      setService(resolution.option.displayName);
      deepLinkRef.current = canonicalID;
      const params = new URLSearchParams(searchParams);
      params.set("service", canonicalID);
      setSearchParams(params, { replace: true });

      const loaded = await loadExposureIngress(client, canonicalID, controller.signal);
      if (version !== requestVersionRef.current) {
        return;
      }
      setIngress(loaded);
      if (loaded.provenance === "unavailable") {
        setError(loaded.error ?? "Service resolution or ingress tracing is unavailable.");
      }
      abortRef.current = null;
      setBusy(false);
    },
    [client, options, searchParams, setSearchParams],
  );

  useEffect(() => {
    const initial = searchParams.get("service")?.trim() ?? "";
    if (!canLoad || initial.length === 0 || deepLinkRef.current === initial) {
      return;
    }
    deepLinkRef.current = initial;
    void runSelection(initial);
  }, [canLoad, runSelection, searchParams]);

  useEffect(
    () => () => {
      requestVersionRef.current += 1;
      abortRef.current?.abort();
    },
    [],
  );

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const name = service.trim();
    if (name.length === 0) {
      setError("Choose an authorized service or paste a canonical workload:… handle.");
      return;
    }
    void runSelection(name);
  }

  const chains = ingress?.chains ?? [];
  const active = chains[selectedChain] ?? chains[0];

  return (
    <div className="page exposure-page">
      <div className="page-intro impact-intro">
        <div>
          <h2>Exposure Path</h2>
          <p>
            Pick an internet-facing entrypoint and follow its proven ingress chain to the runtime —
            each hop opens its evidence and truth level. Posture tiles summarize public entrypoints,
            hops, WAF coverage, and TLS termination.
          </p>
        </div>
        <Badge tone={canLoad ? "teal" : "warn"}>{canLoad ? "live API" : "connect live API"}</Badge>
      </div>

      <form className="exposure-entry-form" onSubmit={submit}>
        <ExposureServiceSelector
          busy={busy}
          onChoose={(option) => {
            invalidateActiveRequest();
            clearDeepLink();
            setService(option.displayName);
          }}
          onValueChange={(value) => {
            invalidateActiveRequest();
            clearDeepLink();
            setService(value);
          }}
          options={options}
          value={service}
        />
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Resolving…" : "Trace ingress"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {error ? (
        <p className="src-err" role="alert">
          {error}
        </p>
      ) : null}

      {busy ? (
        <div className="conn-state compact mt">
          <div aria-hidden className="conn-spinner" />
          <p>Resolving service and tracing ingress chain…</p>
        </div>
      ) : ingress !== null && ingress.provenance === "live" ? (
        <IngressView
          active={active}
          ingress={ingress}
          onSelectChain={(index) => {
            setSelectedChain(index);
            setSelectedHop(null);
          }}
          onSelectHop={setSelectedHop}
          selectedChain={selectedChain}
          selectedHop={selectedHop}
        />
      ) : ingress !== null && ingress.provenance === "empty" && !error ? (
        <NoChainNotice service={ingress.service} />
      ) : ingress === null && !error ? (
        <p className="empty mt">Enter an internet-facing service to trace its ingress chain.</p>
      ) : null}

      <details
        className="exposure-advanced-disclosure"
        onToggle={(e) => setAdvancedOpen((e.target as HTMLDetailsElement).open)}
      >
        <summary>Advanced: handler trace</summary>
        {advancedOpen ? <ExposurePathAdvanced client={client} /> : null}
      </details>
    </div>
  );
}

function selectionError(
  result: Exclude<ExposureServiceSelectionResult, { status: "resolved" }>,
): string {
  switch (result.status) {
    case "ambiguous":
      return `Multiple authorized services match “${result.query}”. Choose one canonical service: ${result.candidates
        .map((candidate) => `${candidate.displayName} (${candidate.canonicalId})`)
        .join(", ")}.`;
    case "not_authorized":
      return "You are not authorized to resolve that service in the active workspace.";
    case "not_found":
      return `No authorized service matches “${result.query}”. Choose an available service or paste a canonical workload:… handle.`;
    case "unavailable":
      return "Service resolution is temporarily unavailable. The prior ingress result was cleared.";
  }
}

function IngressView({
  active,
  ingress,
  onSelectChain,
  onSelectHop,
  selectedChain,
  selectedHop,
}: {
  readonly active: IngressChain | undefined;
  readonly ingress: ExposureIngress;
  readonly onSelectChain: (index: number) => void;
  readonly onSelectHop: (hop: IngressHop) => void;
  readonly selectedChain: number;
  readonly selectedHop: IngressHop | null;
}): React.JSX.Element {
  return (
    <div className="exposure-result mt">
      <div className="exposure-posture-tiles">
        <StatTile
          label="Public entrypoints"
          value={ingress.publicEntrypoints}
          sub="observed public hostnames"
        />
        <StatTile
          label="Hops to service"
          value={active?.hops.length ?? 0}
          sub="on the selected chain"
        />
        <PostureTile
          label="WAF coverage"
          state={ingress.posture.wafCoverage}
          sub={wafSub(ingress)}
        />
        <PostureTile
          label="TLS termination"
          state={ingress.posture.tlsTermination}
          sub={tlsSub(ingress)}
        />
      </div>

      {ingress.chains.length > 1 ? (
        <div className="exposure-chain-options" role="tablist" aria-label="Ingress entrypoints">
          {ingress.chains.map((chain, index) => (
            <button
              aria-pressed={selectedChain === index}
              className="exposure-chain-option"
              key={`${chain.entrypoint}:${index}`}
              onClick={() => onSelectChain(index)}
              type="button"
            >
              {chain.entrypoint || "entrypoint"}
            </button>
          ))}
        </div>
      ) : null}

      <div className="exposure-ingress-layout">
        <Panel
          className="exposure-ingress-panel"
          sub={
            active
              ? `${active.entrypoint} · ${active.visibility || "visibility unknown"}`
              : undefined
          }
          title="Ingress chain"
        >
          {active ? (
            <ol className="exposure-hops" aria-label="Ingress chain hops">
              {active.hops.map((hop, index) => (
                <li className="exposure-hop-item" key={`${hop.id}:${index}`}>
                  <button
                    aria-pressed={selectedHop?.id === hop.id}
                    className={`exposure-hop exposure-hop-${hop.kind}`}
                    onClick={() => onSelectHop(hop)}
                    type="button"
                  >
                    <span className="exposure-hop-kind">{hop.label}</span>
                    <span className="exposure-hop-detail mono">{hop.detail || hop.id}</span>
                    <Badge tone={TRUTH_TONE[hop.truth]}>{hop.truth}</Badge>
                  </button>
                </li>
              ))}
            </ol>
          ) : (
            <p className="empty">No proven ingress chain for this entrypoint.</p>
          )}
        </Panel>

        <aside aria-label="Selected hop evidence" className="exposure-evidence-panel">
          {selectedHop !== null ? (
            <HopEvidence hop={selectedHop} truth={ingress} />
          ) : (
            <div className="exposure-evidence-empty">
              <h3>Hop evidence</h3>
              <p>Select a hop in the chain to inspect its evidence and truth level.</p>
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}

function HopEvidence({
  hop,
  truth,
}: {
  readonly hop: IngressHop;
  readonly truth: ExposureIngress;
}): React.JSX.Element {
  return (
    <div className="exposure-evidence">
      <h3>{hop.label}</h3>
      <dl>
        <div>
          <dt>Node</dt>
          <dd className="mono">{hop.detail || hop.id}</dd>
        </div>
        <div>
          <dt>Kind</dt>
          <dd>{hop.kind.replace(/_/g, " ")}</dd>
        </div>
        <div>
          <dt>Truth level</dt>
          <dd>
            <Badge tone={TRUTH_TONE[hop.truth]}>{hop.truth}</Badge>
          </dd>
        </div>
        {truth.truth ? (
          <div>
            <dt>Capability</dt>
            <dd className="exposure-truth">
              <span className="mono">{truth.truth.capability}</span>
              <FreshDot state={uiFresh(truth.truth.freshness.state)} />
            </dd>
          </div>
        ) : null}
      </dl>
      <p className="exposure-reason">{hop.reason}</p>
      {truth.truth ? <TruthChip level={uiTruth(truth.truth.level)} /> : null}
    </div>
  );
}

function PostureTile({
  label,
  state,
  sub,
}: {
  readonly label: string;
  readonly state: IngressPostureState;
  readonly sub: string;
}): React.JSX.Element {
  return (
    <div className="exposure-posture-tile">
      <div className="exposure-posture-head">{label}</div>
      <div className="exposure-posture-body">
        <Badge tone={POSTURE_TONE[state]}>{POSTURE_LABEL[state]}</Badge>
      </div>
      <div className="exposure-posture-sub">{sub}</div>
    </div>
  );
}

function NoChainNotice({ service }: { readonly service: string }): React.JSX.Element {
  return (
    <Panel className="exposure-unresolved-panel mt" title="No proven ingress chain">
      <p className="exposure-unresolved-lead">
        {service ? <span className="mono">{service}</span> : "This service"} has no materialized
        internet-facing ingress path. No exposure is implied.
      </p>
      <p className="exposure-reason">
        Eshu found no entrypoint-to-runtime network path for this service. Try a service with a
        public hostname, or use the advanced handler trace below.
      </p>
    </Panel>
  );
}

function wafSub(ingress: ExposureIngress): string {
  if (ingress.posture.wafCoverage === "unproven") {
    return "no edge resource materialized";
  }
  return `${ingress.posture.wafProtected}/${ingress.posture.edgeCount} edge web ACL`;
}

function tlsSub(ingress: ExposureIngress): string {
  if (ingress.posture.tlsTermination === "unproven") {
    return "no edge resource materialized";
  }
  return `${ingress.posture.tlsTerminated}/${ingress.posture.edgeCount} ACM cert`;
}
