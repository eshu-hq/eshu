// pages/admin/AdminProvidersPanel.tsx
// IdP providers panel (read-only). Lists the tenant's configured identity
// providers by provider_config_id, provider_kind, and status ONLY. Never
// renders issuer, metadata URL, entity id, client id, or any credential handle.
// On a load error the panel renders "unavailable" rather than fabricated rows.
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../../api/client";
import { loadIdPProviders } from "../../api/adminConsole";
import type { IdPProviderItem } from "../../api/adminConsole";
import { Panel } from "../../components/atoms";
import { dash, truncatedNote } from "./adminFormat";

export function AdminProvidersPanel({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [providers, setProviders] = useState<readonly IdPProviderItem[]>([]);
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
    void loadIdPProviders(client).then((r) => {
      if (cancelled) return;
      setProviders(r.providers);
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
      <Panel title="IdP providers">
        <p className="empty-note">Loading providers…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="IdP providers">
        <p className="unavailable-note">IdP providers unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel title="IdP providers">
      {truncated ? (
        <p className="empty-note">{truncatedNote(truncated, providers.length)}</p>
      ) : null}
      {providers.length === 0 ? (
        <p className="empty-note">No identity providers configured.</p>
      ) : (
        <table className="data-table" aria-label="IdP providers">
          <thead>
            <tr>
              <th>Provider config ID</th>
              <th>Kind</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {providers.map((p) => (
              <tr key={p.provider_config_id}>
                <td>{p.provider_config_id}</td>
                <td>{dash(p.provider_kind)}</td>
                <td>{dash(p.status)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
