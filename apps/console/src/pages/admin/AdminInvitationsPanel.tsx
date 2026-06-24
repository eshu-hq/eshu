// pages/admin/AdminInvitationsPanel.tsx
// Invitations panel: lists the tenant's invitations and offers a Revoke action.
// Renders only ids, role ids, status, and timestamps — never the invite code,
// invitee handle, or inviter identity (all hash-only server-side). On a load
// error the panel renders "unavailable" rather than fabricated rows. Each
// mutation is confirm-then-call, disables its button while in flight, surfaces
// success/failure explicitly, and refetches the list on success.
//
// Stale-load guard: every useEffect load sets `cancelled = true` in its cleanup.
// A refreshKey counter re-triggers the effect after a mutation; client changes
// also re-trigger because client is in the dependency array. Any in-flight load
// from a prior client or prior key checks `cancelled` before committing state.
import { useEffect, useState, useCallback } from "react";

import { fmt, dash, truncatedNote } from "./adminFormat";
import { loadInvitations, revokeInvitation } from "../../api/adminConsole";
import type { InvitationItem } from "../../api/adminConsole";
import type { EshuApiClient } from "../../api/client";
import { Panel, Badge } from "../../components/atoms";

function statusBadge(status: string | undefined): React.JSX.Element {
  if (status === "revoked") return <Badge tone="crit">revoked</Badge>;
  if (status === "accepted") return <Badge tone="teal">accepted</Badge>;
  if (status === "expired") return <Badge tone="neutral">expired</Badge>;
  return <Badge tone="violet">{dash(status)}</Badge>;
}

export function AdminInvitationsPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [items, setItems] = useState<readonly InvitationItem[]>([]);
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
    void loadInvitations(client).then((r) => {
      if (cancelled) return;
      setItems(r.invitations);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client, refreshKey]);

  const onRevoke = useCallback(
    async (inviteId: string) => {
      if (!client) return;
      if (!globalThis.confirm?.(`Revoke invitation ${inviteId}?`)) return;
      setBusyId(inviteId);
      setNotice(null);
      const ok = await revokeInvitation(client, inviteId);
      setBusyId(null);
      if (ok) {
        setNotice(`Invitation ${inviteId} revoked.`);
        setRefreshKey((k) => k + 1);
      } else {
        setNotice(`Failed to revoke invitation ${inviteId}.`);
      }
    },
    [client]
  );

  if (loading) {
    return (
      <Panel title="Invitations">
        <p className="empty-note">Loading invitations…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="Invitations">
        <p className="unavailable-note">Invitations unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="Invitations">
      {notice ? <p className="empty-note" role="status">{notice}</p> : null}
      {truncated ? <p className="empty-note">{truncatedNote(truncated, items.length)}</p> : null}
      {items.length === 0 ? (
        <p className="empty-note">No invitations found.</p>
      ) : (
        <table className="data-table" aria-label="Invitations">
          <thead>
            <tr>
              <th>Invite ID</th>
              <th>Role</th>
              <th>Status</th>
              <th>Expires</th>
              <th>Created</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {items.map((inv) => {
              const terminal =
                inv.status === "revoked" ||
                inv.status === "accepted" ||
                inv.status === "expired";
              return (
                <tr key={inv.invite_id}>
                  <td>{inv.invite_id}</td>
                  <td>{dash(inv.role_id)}</td>
                  <td>{statusBadge(inv.status)}</td>
                  <td>{fmt(inv.expires_at)}</td>
                  <td>{fmt(inv.created_at)}</td>
                  <td>
                    <button
                      type="button"
                      className="btn-ghost"
                      disabled={busyId === inv.invite_id || terminal}
                      onClick={() => void onRevoke(inv.invite_id)}
                    >
                      {busyId === inv.invite_id ? "Revoking…" : "Revoke"}
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
