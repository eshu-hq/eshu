import {
  type ExposureIngress,
  type IngressChain,
  type IngressHop,
  type IngressPostureState,
  type IngressTruth,
} from "../api/exposureIngress";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";

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

export function ExposureIngressView({
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
              ? [
                  active.entrypoint,
                  active.visibility || "visibility unknown",
                  active.environment || "environment unknown",
                ].join(" · ")
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

export function NoExposureChainNotice({
  ingress,
}: {
  readonly ingress: ExposureIngress;
}): React.JSX.Element {
  return (
    <div className="exposure-result mt">
      <div className="exposure-posture-tiles">
        <StatTile
          label="Public entrypoints"
          value={ingress.publicEntrypoints}
          sub="observed public hostnames"
        />
        <StatTile label="Hops to service" value={ingress.totalHops} sub="proven ingress hops" />
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
      <Panel className="exposure-unresolved-panel" title="No proven ingress chain">
        <p className="exposure-unresolved-lead">
          {ingress.service ? <span className="mono">{ingress.service}</span> : "This service"} has
          no materialized internet-facing ingress path. No exposure is implied.
        </p>
        <p className="exposure-reason">
          Eshu found no entrypoint-to-runtime network path for this service. Try a service with a
          public hostname, or use the advanced handler trace below.
        </p>
        {ingress.truth ? (
          <div className="exposure-empty-truth">
            <span className="mono">{ingress.truth.capability}</span>
            <FreshDot state={uiFresh(ingress.truth.freshness.state)} />
            <TruthChip level={uiTruth(ingress.truth.level)} />
          </div>
        ) : null}
      </Panel>
    </div>
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
