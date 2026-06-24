// pages/admin/AdminAssignmentsPanel.tsx
// Role-assignment panel: lists membership-role assignments and offers a Grant
// form plus a per-row Revoke. Renders ids, source, status, and timestamps only.
// On a load error the panel renders "unavailable" rather than fabricated rows.
// Mutations are confirm-then-call, disable while in flight, surface
// success/failure explicitly, and refetch on success.
//
// Stale-load guard: every useEffect load sets `cancelled = true` in its cleanup.
// A refreshKey counter re-triggers the effect after a mutation; client changes
// also re-trigger because client is in the dependency array. Any in-flight load
// from a prior client or prior key checks `cancelled` before committing state.
import { useEffect, useState, useCallback } from "react";

import { fmt, dash, truncatedNote } from "./adminFormat";
import {
  loadRoleAssignments,
  grantRoleAssignment,
  revokeRoleAssignment
} from "../../api/adminConsole";
import type { RoleAssignmentItem } from "../../api/adminConsole";
import type { EshuApiClient } from "../../api/client";
import { Panel, Badge } from "../../components/atoms";

function statusBadge(status: string | undefined): React.JSX.Element {
  if (status === "revoked") return <Badge tone="crit">revoked</Badge>;
  if (status === "active") return <Badge tone="teal">active</Badge>;
  return <Badge tone="neutral">{dash(status)}</Badge>;
}

export function AdminAssignmentsPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [items, setItems] = useState<readonly RoleAssignmentItem[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [grantUser, setGrantUser] = useState("");
  const [grantRole, setGrantRole] = useState("");
  const [grantWorkspace, setGrantWorkspace] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadRoleAssignments(client).then((r) => {
      if (cancelled) return;
      setItems(r.assignments);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client, refreshKey]);

  const onGrant = useCallback(async () => {
    if (!client || grantUser.length === 0 || grantRole.length === 0) return;
    if (!globalThis.confirm?.(`Grant role ${grantRole} to user ${grantUser}?`)) return;
    setBusy(true);
    setNotice(null);
    const ok = await grantRoleAssignment(client, {
      user_id: grantUser,
      role_id: grantRole,
      workspace_id: grantWorkspace
    });
    setBusy(false);
    if (ok) {
      setNotice(`Granted ${grantRole} to ${grantUser}.`);
      setGrantUser("");
      setGrantRole("");
      setGrantWorkspace("");
      setRefreshKey((k) => k + 1);
    } else {
      setNotice(`Failed to grant ${grantRole} to ${grantUser}.`);
    }
  }, [client, grantUser, grantRole, grantWorkspace]);

  const onRevoke = useCallback(
    async (item: RoleAssignmentItem) => {
      if (!client) return;
      if (!globalThis.confirm?.(`Revoke role ${item.role_id} from user ${item.user_id}?`)) return;
      setBusy(true);
      setNotice(null);
      const ok = await revokeRoleAssignment(client, {
        user_id: item.user_id,
        role_id: item.role_id,
        workspace_id: item.workspace_id
      });
      setBusy(false);
      if (ok) {
        setNotice(`Revoked ${item.role_id} from ${item.user_id}.`);
        setRefreshKey((k) => k + 1);
      } else {
        setNotice(`Failed to revoke ${item.role_id} from ${item.user_id}.`);
      }
    },
    [client]
  );

  const grantForm = (
    <form
      className="admin-form"
      aria-label="Grant role assignment"
      onSubmit={(e) => {
        e.preventDefault();
        void onGrant();
      }}
    >
      <input
        aria-label="User ID"
        placeholder="User ID"
        value={grantUser}
        onChange={(e) => setGrantUser(e.target.value)}
      />
      <input
        aria-label="Role ID"
        placeholder="Role ID"
        value={grantRole}
        onChange={(e) => setGrantRole(e.target.value)}
      />
      <input
        aria-label="Workspace ID (optional)"
        placeholder="Workspace ID (optional)"
        value={grantWorkspace}
        onChange={(e) => setGrantWorkspace(e.target.value)}
      />
      <button
        type="submit"
        className="btn-ghost"
        disabled={busy || grantUser.length === 0 || grantRole.length === 0}
      >
        {busy ? "Working…" : "Grant"}
      </button>
    </form>
  );

  if (loading) {
    return (
      <Panel title="Role assignments">
        <p className="empty-note">Loading role assignments…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="Role assignments">
        {grantForm}
        <p className="unavailable-note">Role assignments unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="Role assignments">
      {grantForm}
      {notice ? <p className="empty-note" role="status">{notice}</p> : null}
      {truncated ? <p className="empty-note">{truncatedNote(truncated, items.length)}</p> : null}
      {items.length === 0 ? (
        <p className="empty-note">No role assignments found.</p>
      ) : (
        <table className="data-table" aria-label="Role assignments">
          <thead>
            <tr>
              <th>User</th>
              <th>Role</th>
              <th>Source</th>
              <th>Status</th>
              <th>Effective</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {items.map((a, i) => (
              <tr key={`${a.user_id}:${a.role_id}:${i}`}>
                <td>{a.user_id}</td>
                <td>{a.role_id}</td>
                <td>{dash(a.assignment_source)}</td>
                <td>{statusBadge(a.status)}</td>
                <td>{fmt(a.effective_at)}</td>
                <td>
                  <button
                    type="button"
                    className="btn-ghost"
                    disabled={busy || a.status === "revoked"}
                    onClick={() => void onRevoke(a)}
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
