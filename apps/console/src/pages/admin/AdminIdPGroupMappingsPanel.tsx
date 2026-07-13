// pages/admin/AdminIdPGroupMappingsPanel.tsx
// IdP group→role mappings panel: lists mappings and offers a Create form plus a
// per-row Delete. Renders only the opaque mapping_ref, provider config id, role
// id, status, and timestamps — NEVER the external group name (it is hashed
// server-side and never returned). The Create form does accept a raw external
// group name as input (the server hashes it once); that input is cleared after
// submit and never retained or rendered back. On a load error the panel renders
// "unavailable" rather than fabricated rows. Mutations are confirm-then-call,
// disable while in flight, surface success/failure, and refetch on success.
//
// Stale-load guard: every useEffect load sets `cancelled = true` in its cleanup.
// A refreshKey counter re-triggers the effect after a mutation; client changes
// also re-trigger because client is in the dependency array. Any in-flight load
// from a prior client or prior key checks `cancelled` before committing state.
import { useEffect, useState, useCallback } from "react";

import { fmt, dash, truncatedNote } from "./adminFormat";
import {
  loadIdPGroupMappings,
  createIdPGroupMapping,
  deleteIdPGroupMapping,
} from "../../api/adminConsole";
import type { IdPGroupMappingItem } from "../../api/adminConsole";
import type { EshuApiClient } from "../../api/client";
import { Panel, Badge } from "../../components/atoms";
import { useConfirm } from "../../components/useConfirm";

function statusBadge(status: string | undefined): React.JSX.Element {
  if (status === "active") return <Badge tone="teal">active</Badge>;
  if (status === "revoked" || status === "deleted") return <Badge tone="crit">{status}</Badge>;
  return <Badge tone="neutral">{dash(status)}</Badge>;
}

export function AdminIdPGroupMappingsPanel({
  client,
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [items, setItems] = useState<readonly IdPGroupMappingItem[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [provider, setProvider] = useState("");
  const [externalGroup, setExternalGroup] = useState("");
  const [role, setRole] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const { confirm, confirmDialog } = useConfirm();

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadIdPGroupMappings(client).then((r) => {
      if (cancelled) return;
      setItems(r.mappings);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client, refreshKey]);

  const onCreate = useCallback(async () => {
    if (!client || provider.length === 0 || externalGroup.length === 0 || role.length === 0) {
      return;
    }
    if (!(await confirm(`Create mapping for provider ${provider} → role ${role}?`))) return;
    setBusy(true);
    setNotice(null);
    const ok = await createIdPGroupMapping(client, {
      provider_config_id: provider,
      external_group: externalGroup,
      role_id: role,
      workspace_id: workspace,
    });
    setBusy(false);
    if (ok) {
      setNotice(`Mapping created for ${provider} → ${role}.`);
      setProvider("");
      // Clear the raw external group input immediately; never retain it.
      setExternalGroup("");
      setRole("");
      setWorkspace("");
      setRefreshKey((k) => k + 1);
    } else {
      setNotice(`Failed to create mapping for ${provider} → ${role}.`);
    }
  }, [client, provider, externalGroup, role, workspace, confirm]);

  const onDelete = useCallback(
    async (mappingRef: string) => {
      if (!client) return;
      if (
        !(await confirm(`Delete mapping ${mappingRef}?`, { danger: true, confirmLabel: "Delete" }))
      )
        return;
      setBusy(true);
      setNotice(null);
      const ok = await deleteIdPGroupMapping(client, mappingRef);
      setBusy(false);
      if (ok) {
        setNotice(`Mapping ${mappingRef} deleted.`);
        setRefreshKey((k) => k + 1);
      } else {
        setNotice(`Failed to delete mapping ${mappingRef}.`);
      }
    },
    [client, confirm],
  );

  const createForm = (
    <form
      className="admin-form"
      aria-label="Create group mapping"
      onSubmit={(e) => {
        e.preventDefault();
        void onCreate();
      }}
    >
      <input
        aria-label="Provider config ID"
        placeholder="Provider config ID"
        value={provider}
        onChange={(e) => setProvider(e.target.value)}
      />
      <input
        aria-label="External group"
        placeholder="External group"
        value={externalGroup}
        onChange={(e) => setExternalGroup(e.target.value)}
      />
      <input
        aria-label="Role ID"
        placeholder="Role ID"
        value={role}
        onChange={(e) => setRole(e.target.value)}
      />
      <input
        aria-label="Workspace ID (optional)"
        placeholder="Workspace ID (optional)"
        value={workspace}
        onChange={(e) => setWorkspace(e.target.value)}
      />
      <button
        type="submit"
        className="btn-ghost"
        disabled={busy || provider.length === 0 || externalGroup.length === 0 || role.length === 0}
      >
        {busy ? "Working…" : "Create"}
      </button>
    </form>
  );

  if (loading) {
    return (
      <Panel title="IdP group mappings">
        <p className="empty-note">Loading group mappings…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="IdP group mappings">
        {createForm}
        {confirmDialog}
        <p className="unavailable-note">IdP group mappings unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="IdP group mappings">
      {createForm}
      {confirmDialog}
      {notice ? (
        <p className="empty-note" role="status">
          {notice}
        </p>
      ) : null}
      {truncated ? <p className="empty-note">{truncatedNote(truncated, items.length)}</p> : null}
      {items.length === 0 ? (
        <p className="empty-note">No group mappings found.</p>
      ) : (
        <table className="data-table" aria-label="IdP group mappings">
          <thead>
            <tr>
              <th>Mapping ref</th>
              <th>Provider</th>
              <th>Role</th>
              <th>Status</th>
              <th>Effective</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {items.map((m) => (
              <tr key={m.mapping_ref}>
                <td>{m.mapping_ref}</td>
                <td>{dash(m.provider_config_id)}</td>
                <td>{dash(m.role_id)}</td>
                <td>{statusBadge(m.status)}</td>
                <td>{fmt(m.effective_at)}</td>
                <td>
                  <button
                    type="button"
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => void onDelete(m.mapping_ref)}
                  >
                    Delete
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
