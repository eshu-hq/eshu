import { useEffect, useMemo, useState } from "react";

import type { EshuApiClient } from "../api/client";
import { loadCloudInventory } from "../api/cloudInventory";
import type { CloudInventoryPage, CloudInventoryRow } from "../api/cloudInventory";
import { Badge, Panel, TruthChip, FreshDot } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";

const INVENTORY_LIMIT = 50;

export function CloudInventoryPanel({
  accountId,
  client,
  provider
}: {
  readonly accountId: string;
  readonly client?: EshuApiClient;
  readonly provider: string;
}): React.JSX.Element | null {
  const [page, setPage] = useState<CloudInventoryPage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setPage(null);
      setErr("");
      setBusy(false);
      return () => { cancelled = true; };
    }
    setBusy(true);
    setErr("");
    void loadCloudInventory(client, {
      accountId: accountId.trim() || undefined,
      limit: INVENTORY_LIMIT,
      provider: provider.trim() || undefined
    })
      .then((result) => {
        if (!cancelled) {
          setPage(result);
          setBusy(false);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setPage(null);
          setBusy(false);
          setErr(error instanceof Error ? error.message : "failed to load cloud inventory");
        }
      });
    return () => { cancelled = true; };
  }, [accountId, client, provider]);

  const rows = page?.rows ?? [];
  const states = useMemo(() => countBy(rows, (row) => row.sourceState || "unknown"), [rows]);
  const origins = useMemo(() => countBy(rows, (row) => row.managementOrigin || "unknown"), [rows]);

  return (
    <Panel
      className="mt"
      title="Canonical inventory"
      sub={page ? `${page.count} canonical identities${page.truncated ? " · more available" : ""}` : busy ? "loading canonical identities" : "reducer-owned source-state readback"}
      action={page ? <div className="row" style={{ gap: 8 }}><TruthChip level={uiTruth(page.truth.level)} /><FreshDot state={uiFresh(page.truth.freshness)} /></div> : null}
    >
      {err ? <p className="empty" style={{ textAlign: "left" }}>Canonical inventory unavailable: {err}</p> : null}
      {busy && page === null ? (
        <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading canonical cloud inventory...</p></div>
      ) : null}
      {rows.length > 0 ? (
        <>
          <div className="row" style={{ gap: 6, flexWrap: "wrap", marginBottom: 12 }}>
            {states.map((state) => <Badge key={state.key} tone={state.key === "exact" ? "teal" : "neutral"}>{state.key} · {state.count}</Badge>)}
            {origins.map((origin) => <Badge key={origin.key} tone="neutral">{origin.key} · {origin.count}</Badge>)}
          </div>
          <div className="table-scroll">
            <table className="tbl">
              <thead><tr><th>Resource type</th><th>Provider</th><th>Scope</th><th>Source state</th><th>Evidence</th></tr></thead>
              <tbody>
                {rows.slice(0, 8).map((row) => (
                  <tr key={row.cloudResourceUid}>
                    <td className="mono" style={{ fontSize: ".76rem" }}>{row.resourceType || "—"}</td>
                    <td className="t-name">{row.provider || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{row.scopeId || "—"}</td>
                    <td><Badge tone={row.sourceState === "exact" ? "teal" : "neutral"}>{row.sourceState || "unknown"}</Badge></td>
                    <td className="t-mut" style={{ fontSize: ".76rem" }}>{evidenceText(row)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : null}
      {!busy && !err && page !== null && rows.length === 0 ? (
        <p className="empty" style={{ textAlign: "left" }}>No canonical cloud inventory rows matched this scope.</p>
      ) : null}
    </Panel>
  );
}

function evidenceText(row: CloudInventoryRow): string {
  const labels = [
    row.evidence.declared ? "declared" : "",
    row.evidence.applied ? "applied" : "",
    row.evidence.observed ? "observed" : ""
  ].filter(Boolean);
  return labels.length > 0 ? labels.join(" · ") : "none";
}

function countBy(rows: readonly CloudInventoryRow[], key: (row: CloudInventoryRow) => string): readonly { readonly key: string; readonly count: number }[] {
  const counts = new Map<string, number>();
  for (const row of rows) {
    const group = key(row);
    counts.set(group, (counts.get(group) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([k, count]) => ({ key: k, count }))
    .sort((a, b) => b.count - a.count || a.key.localeCompare(b.key));
}
