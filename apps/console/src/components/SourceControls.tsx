import { useState } from "react";

// Connection lifecycle. Private mode requires a live API; "needs-connection" is
// the initial state when no saved environment exists, and "error" is a failed
// private connect. Demo mode is an explicit connected fixture source.
export type ConnStatus = "needs-connection" | "connecting" | "connected" | "error";

export interface SourceState {
  readonly base: string;
  readonly key: string;
  readonly mode: "demo" | "private";
  readonly status: ConnStatus;
  readonly msg: string;
}

// ConnectionState renders the non-connected private lifecycle: a loading spinner
// while connecting, or a prompt to connect when there is no saved source.
export function ConnectionState({
  status,
  onConnect
}: {
  readonly status: ConnStatus;
  readonly onConnect: () => void;
}): React.JSX.Element {
  if (status === "connecting") {
    return (
      <div className="conn-state" role="status" aria-live="polite">
        <div className="conn-spinner" aria-hidden />
        <p>Connecting to the Eshu API…</p>
      </div>
    );
  }
  const title = status === "error" ? "No live data" : "Connect to a live Eshu API";
  const detail = status === "error"
    ? "The last connection attempt failed. Check the base URL and API credential, then reconnect."
    : "Enter your Eshu API base URL for private data, or choose the explicit demo fixture source.";
  return (
    <div className="conn-state">
      <h2>{title}</h2>
      <p>{detail}</p>
      <button className="btn-ghost active" onClick={onConnect}>Open data source</button>
    </div>
  );
}

export function SourcePopover({
  source,
  onConnect,
  onDemo,
  onClose
}: {
  readonly source: SourceState;
  readonly onConnect: (base: string, key: string) => void;
  readonly onDemo: () => void;
  readonly onClose: () => void;
}): React.JSX.Element {
  const [base, setBase] = useState(source.mode === "demo" ? "/eshu-api/" : source.base || "/eshu-api/");
  const [key, setKey] = useState(source.key || "");
  return (
    <>
      <div className="popover-scrim" onClick={onClose} />
      <div className="popover" role="dialog" aria-label="Data source">
        <div className="popover-head"><strong>Data source</strong><span className="t-mut" style={{ fontSize: ".72rem" }}>read-only · {source.mode === "demo" ? "demo" : "live"}</span></div>
        <div className={`source-opt col${source.mode === "private" ? " active" : ""}`}>
          <div><strong>Live Eshu API</strong><span>application/eshu.envelope+json</span></div>
          <div className="row" style={{ gap: 6, marginTop: 8 }}>
            <input className="popover-input mono" value={base} onChange={(e) => setBase(e.target.value)} placeholder="/eshu-api/" />
            <button className="btn-ghost active" onClick={() => onConnect(base, key)}>Connect</button>
          </div>
          <input className="popover-input mono" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="API credential" style={{ width: "100%", marginTop: 6 }} autoComplete="off" />
          {source.status === "error" ? <span className="src-err">⚠ {source.msg || "unreachable"}</span> : null}
          {source.status === "connected" && source.mode === "private" ? <span className="src-ok">✓ connected</span> : null}
          {source.status === "connecting" ? <span className="t-mut" style={{ fontSize: ".72rem" }}>connecting…</span> : null}
        </div>
        <div className={`source-opt col${source.mode === "demo" ? " active" : ""}`}>
          <div><strong>Prospect demo</strong><span>Typed fixtures; no live API or real workspace data.</span></div>
          <button className="btn-ghost active" type="button" onClick={onDemo}>Use demo fixtures</button>
        </div>
        <p className="t-mut" style={{ fontSize: ".7rem", margin: "4px 2px 0", lineHeight: 1.5 }}>The console dev server proxies <span className="mono">/eshu-api/</span> → <span className="mono">127.0.0.1:8080</span>. Credential is exchanged for a browser session.</p>
      </div>
    </>
  );
}
