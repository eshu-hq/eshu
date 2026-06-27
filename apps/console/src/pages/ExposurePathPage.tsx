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
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { ExposurePathAdvanced } from "./ExposurePathAdvanced";
import type { EshuApiClient } from "../api/client";
import {
  loadExposureIngress,
  type ExposureIngress,
  type IngressChain,
  type IngressHop,
  type IngressPostureState,
  type IngressTruth,
} from "../api/exposureIngress";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";
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
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [service, setService] = useState(() => searchParams.get("service") ?? "");
  const [ingress, setIngress] = useState<ExposureIngress | null>(null);
  const [selectedChain, setSelectedChain] = useState(0);
  const [selectedHop, setSelectedHop] = useState<IngressHop | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const canLoad = client !== undefined;

  const runIngress = useCallback(
    async (name: string) => {
      const trimmed = name.trim();
      if (!client || trimmed.length === 0) {
        return;
      }
      setBusy(true);
      setError("");
      try {
        const loaded = await loadExposureIngress(client, trimmed);
        setIngress(loaded);
        setSelectedChain(0);
        setSelectedHop(null);
        if (loaded.provenance === "unavailable") {
          setError(loaded.error ?? "failed to trace ingress chain");
        }
      } catch (ingressError) {
        setIngress(null);
        setError(
          ingressError instanceof Error ? ingressError.message : "failed to trace ingress chain",
        );
      } finally {
        setBusy(false);
      }
    },
    [client],
  );

  // Auto-load on mount when the client is already available and a service is in
  // the URL (the common case for deep-links in an already-connected session).
  useEffect(() => {
    const initial = searchParams.get("service")?.trim() ?? "";
    if (initial.length > 0 && canLoad) {
      void runIngress(initial);
    }
    // Run once on mount; subsequent loads are user-driven via submit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Re-trigger the load when the client connects after mount (boot race: the
  // page mounts before the saved private env completes its handshake, so
  // `client` is undefined at mount and the effect above skips the load).
  // Guard: only fire when the ingress hasn't loaded yet and a service is in the
  // URL, so a user-driven submit is never overwritten by this effect.
  useEffect(() => {
    if (!canLoad || ingress !== null || busy) {
      return;
    }
    const initial = searchParams.get("service")?.trim() ?? "";
    if (initial.length > 0) {
      void runIngress(initial);
    }
    // Depend on client readiness (canLoad) so this fires exactly once when the
    // client transitions from undefined → defined after mount. ingress/busy are
    // guards only; runIngress is stable (useCallback on [client]).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canLoad]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const name = service.trim();
    if (name.length === 0) {
      setError("A service name is required to trace its ingress chain.");
      return;
    }
    const params = new URLSearchParams(searchParams);
    params.set("service", name);
    setSearchParams(params);
    void runIngress(name);
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
        <label className="exposure-entry-field">
          <span>Service entrypoint</span>
          <input
            aria-label="Service name"
            className="popover-input mono"
            onChange={(event) => setService(event.target.value)}
            placeholder="checkout"
            value={service}
          />
        </label>
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Tracing…" : "Trace ingress"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {error ? <p className="src-err">{error}</p> : null}

      {busy ? (
        <div className="conn-state compact mt">
          <div aria-hidden className="conn-spinner" />
          <p>Tracing ingress chain…</p>
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
