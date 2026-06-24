// pages/admin/AdminTokensPanel.tsx
// API tokens panel: lists every user's generated API tokens within the tenant
// and offers a per-row Revoke. Renders token id, class, owning user/service
// principal, status, and timestamps ONLY — never the token hash or display
// label hash. On a load error the panel renders "unavailable" rather than
// fabricated rows. Revoke is confirm-then-call, disables while in flight,
// surfaces success/failure, and refetches on success.
//
// Stale-load guard: every useEffect load sets `cancelled = true` in its cleanup.
// A refreshKey counter re-triggers the effect after a mutation; client changes
// also re-trigger because client is in the dependency array. Any in-flight load
// from a prior client or prior key checks `cancelled` before committing state.
import { useEffect, useState, useCallback } from "react";

import { fmt, dash, truncatedNote } from "./adminFormat";
import { loadApiTokens, revokeApiToken } from "../../api/adminConsole";
import type { AdminAPITokenItem } from "../../api/adminConsole";
import type { EshuApiClient } from "../../api/client";
import { Panel, Badge } from "../../components/atoms";

function statusBadge(status: string | undefined, revokedAt: string | undefined): React.JSX.Element {
  if (revokedAt || status === "revoked") return <Badge tone="crit">revoked</Badge>;
  if (status === "active") return <Badge tone="teal">active</Badge>;
  return <Badge tone="neutral">{dash(status)}</Badge>;
}

export function AdminTokensPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [items, setItems] = useState<readonly AdminAPITokenItem[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadApiTokens(client).then((r) => {
      if (cancelled) return;
      setItems(r.tokens);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client, refreshKey]);

  const onRevoke = useCallback(
    async (tokenId: string) => {
      if (!client) return;
      if (!globalThis.confirm?.(`Revoke API token ${tokenId}?`)) return;
      setBusyId(tokenId);
      setNotice(null);
      const ok = await revokeApiToken(client, tokenId);
      setBusyId(null);
      if (ok) {
        setNotice(`Token ${tokenId} revoked.`);
        setRefreshKey((k) => k + 1);
      } else {
        setNotice(`Failed to revoke token ${tokenId}.`);
      }
    },
    [client]
  );

  if (loading) {
    return (
      <Panel title="API tokens">
        <p className="empty-note">Loading tokens…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="API tokens">
        <p className="unavailable-note">API tokens unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="API tokens">
      {notice ? <p className="empty-note" role="status">{notice}</p> : null}
      {truncated ? <p className="empty-note">{truncatedNote(truncated, items.length)}</p> : null}
      {items.length === 0 ? (
        <p className="empty-note">No API tokens found.</p>
      ) : (
        <table className="data-table" aria-label="API tokens">
          <thead>
            <tr>
              <th>Token ID</th>
              <th>Class</th>
              <th>Owner</th>
              <th>Status</th>
              <th>Issued</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => {
              const revoked = Boolean(t.revoked_at) || t.status === "revoked";
              return (
                <tr key={t.token_id}>
                  <td>{t.token_id}</td>
                  <td>{dash(t.token_class)}</td>
                  <td>{dash(t.user_id ?? t.service_principal_id)}</td>
                  <td>{statusBadge(t.status, t.revoked_at)}</td>
                  <td>{fmt(t.issued_at)}</td>
                  <td>
                    <button
                      type="button"
                      className="btn-ghost"
                      disabled={busyId === t.token_id || revoked}
                      onClick={() => void onRevoke(t.token_id)}
                    >
                      {busyId === t.token_id ? "Revoking…" : "Revoke"}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
