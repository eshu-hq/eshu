// pages/admin/AdminRolesPanel.tsx
// Roles & grants panel (read-only). Lists the tenant's roles and the capability
// grants each role confers: role id, status, built-in flag, and per-grant
// action/feature/data class/scope class/status. Renders no role key hash,
// policy revision hash, or hashed scope selector. On a load error the panel
// renders "unavailable" rather than fabricated rows.
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../../api/client";
import { loadRoles } from "../../api/adminConsole";
import type { RoleItem } from "../../api/adminConsole";
import { Panel, Badge } from "../../components/atoms";
import { dash, truncatedNote } from "./adminFormat";

export function AdminRolesPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [roles, setRoles] = useState<readonly RoleItem[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    void loadRoles(client).then((r) => {
      if (cancelled) return;
      setRoles(r.roles);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  if (loading) {
    return (
      <Panel title="Roles & grants">
        <p className="empty-note">Loading roles…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="Roles & grants">
        <p className="unavailable-note">Roles unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="Roles & grants">
      {truncated ? <p className="empty-note">{truncatedNote(truncated, roles.length)}</p> : null}
      {roles.length === 0 ? (
        <p className="empty-note">No roles found.</p>
      ) : (
        <table className="data-table" aria-label="Roles and grants">
          <thead>
            <tr>
              <th>Role</th>
              <th>Status</th>
              <th>Built-in</th>
              <th>Grants (action/feature/data/scope)</th>
            </tr>
          </thead>
          <tbody>
            {roles.map((role) => (
              <tr key={role.role_id}>
                <td>{role.role_id}</td>
                <td>{dash(role.status)}</td>
                <td>
                  {role.built_in ? (
                    <Badge tone="violet">built-in</Badge>
                  ) : (
                    <Badge tone="neutral">custom</Badge>
                  )}
                </td>
                <td>
                  {(role.grants ?? []).length === 0
                    ? "—"
                    : (role.grants ?? []).map((g, i) => (
                        <div key={g.grant_id ?? i} className="grant-line">
                          {dash(g.action)} / {dash(g.feature)} / {dash(g.data_class)} /{" "}
                          {dash(g.scope_class)}
                        </div>
                      ))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
