// pages/admin/AdminProvidersPanel.tsx
// Identity providers panel (#4967, consumes the #4966 DB-backed provider-config
// CRUD API — replaces the prior read-only id/kind/status-only panel). Lists
// every configured OIDC/SAML provider with a derived label, kind, status pill,
// group-mapping count, and secret-rotation date; row actions are Test, Edit,
// and Disable. Add/Edit opens ProviderConfigDrawer. Env-managed providers
// (managed_by === "environment") are visible and testable but Edit/Disable are
// disabled — they are defined by deployment config, not editable here.
//
// "Last login" recency has no backing field anywhere in the #4966 API surface
// (AdminProviderConfigDetail carries no per-provider login timestamp) — it
// renders "—" via dash() rather than a fabricated value, the same convention
// every other admin panel here uses for an absent optional field. "Secret
// rotation" uses updated_at: the backend requires the full secret be
// resupplied on every create/update (admin_provider_config_build.go), so
// updated_at IS the date the secret was last (re)set, not merely a proxy.
//
// On a load error the panel renders "unavailable" rather than fabricated rows,
// matching every other admin panel's convention.
//
// Stale-load guard: every useEffect load sets `cancelled = true` in its
// cleanup. A refreshKey counter re-triggers the effect after a mutation or a
// drawer save; client changes also re-trigger because client is in the
// dependency array. Any in-flight load from a prior client or prior key
// checks `cancelled` before committing state.
//
// ProviderConfigDrawer (plus its OIDC/SAML field subcomponents) is loaded via
// React.lazy/dynamic import — it is only needed when an admin clicks Add or
// Edit, so keeping it out of the eager main chunk matters for the console
// bundle-budget gate (scripts/console-bundle-budget.mjs), the same pattern
// WorkspacePage already uses in appRoutes.tsx.
import { useEffect, useState, useCallback, lazy, Suspense } from "react";

import { fmt, dash } from "./adminFormat";
import { loadIdPGroupMappings } from "../../api/adminConsole";
import {
  loadProviderConfigs,
  disableProviderConfig,
  testProviderConfigConnection,
  deriveProviderLabel,
} from "../../api/adminProviderConfig";
import type { AdminProviderConfigItem } from "../../api/adminProviderConfig";
import type { EshuApiClient } from "../../api/client";
import { Panel, Badge } from "../../components/atoms";

const ProviderConfigDrawer = lazy(() =>
  import("./ProviderConfigDrawer").then((m) => ({ default: m.ProviderConfigDrawer })),
);

function kindLabel(kind: string): string {
  return kind === "external_saml" ? "SAML" : "OIDC";
}

function statusBadge(status: string): React.JSX.Element {
  if (status === "active") return <Badge tone="teal">active</Badge>;
  return <Badge tone="neutral">{dash(status)}</Badge>;
}

// countMappingsByProvider is a best-effort supplement to the provider list —
// it never blocks or fails the provider table. A load failure here simply
// leaves every provider's count at "—" (via the caller's fallback), the same
// "no fabricated data" convention as every other loader in this file.
async function countMappingsByProvider(client: EshuApiClient): Promise<Map<string, number>> {
  const result = await loadIdPGroupMappings(client);
  const counts = new Map<string, number>();
  if (result.provenance !== "live") return counts;
  for (const m of result.mappings) {
    if (!m.provider_config_id) continue;
    counts.set(m.provider_config_id, (counts.get(m.provider_config_id) ?? 0) + 1);
  }
  return counts;
}

export function AdminProvidersPanel({
  client,
  baseUrl,
}: {
  readonly client?: EshuApiClient;
  readonly baseUrl?: string;
}): React.JSX.Element {
  const [items, setItems] = useState<readonly AdminProviderConfigItem[]>([]);
  const [mappingCounts, setMappingCounts] = useState<Map<string, number>>(new Map());
  const [truncated, setTruncated] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [drawer, setDrawer] = useState<{
    mode: "create" | "edit";
    item?: AdminProviderConfigItem;
  } | null>(null);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setUnavailable(true);
      setLoading(false);
      return;
    }
    setLoading(true);
    void loadProviderConfigs(client).then((r) => {
      if (cancelled) return;
      setItems(r.items);
      setTruncated(r.truncated);
      setUnavailable(r.provenance === "unavailable");
      setLoading(false);
      setLoaded(true);
    });
    void countMappingsByProvider(client).then((counts) => {
      if (cancelled) return;
      setMappingCounts(counts);
    });
    return () => {
      cancelled = true;
    };
  }, [client, refreshKey]);

  const onTest = useCallback(
    async (item: AdminProviderConfigItem) => {
      if (!client) return;
      setBusyId(item.provider_config_id);
      setNotice(null);
      const result = await testProviderConfigConnection(client, item.provider_config_id);
      setBusyId(null);
      setNotice(
        result.ok
          ? `Test sign-in passed for ${item.provider_config_id}.${result.detail ? ` ${result.detail}` : ""}`
          : `Test sign-in failed for ${item.provider_config_id}.${result.detail ? ` ${result.detail}` : ""}`,
      );
    },
    [client],
  );

  const onDisable = useCallback(
    async (item: AdminProviderConfigItem) => {
      if (!client) return;
      if (!globalThis.confirm?.(`Disable provider ${item.provider_config_id}?`)) return;
      setBusyId(item.provider_config_id);
      setNotice(null);
      const outcome = await disableProviderConfig(client, item.provider_config_id);
      setBusyId(null);
      if (outcome.ok) {
        setNotice(`Provider ${item.provider_config_id} disabled.`);
        setRefreshKey((k) => k + 1);
      } else {
        setNotice(outcome.errorMessage ?? `Failed to disable ${item.provider_config_id}.`);
      }
    },
    [client],
  );

  // Only the INITIAL load (loaded still false) renders the loading-only
  // Panel, which omits the drawer JSX below. A refresh (refreshKey bump from
  // a mutation or a drawer save — e.g. onRunTest's onSaved()) must not
  // unmount an already-open drawer while it reloads: doing so wiped the
  // admin's typed fields, including the write-only client secret, and
  // remounted the drawer in create mode with Save permanently disabled
  // (#5033). The table below keeps showing the previous rows during a
  // refresh, which is also the better UX.
  if (loading && !loaded) {
    return (
      <Panel title="Providers">
        <p className="empty-note">Loading providers…</p>
      </Panel>
    );
  }
  if (unavailable) {
    return (
      <Panel title="Providers">
        <p className="unavailable-note">Providers unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel
      title="Providers"
      action={
        client ? (
          <button
            type="button"
            className="btn-ghost active"
            onClick={() => setDrawer({ mode: "create" })}
          >
            Add provider
          </button>
        ) : null
      }
    >
      {notice ? (
        <p className="empty-note" role="status">
          {notice}
        </p>
      ) : null}
      {truncated ? (
        <p className="empty-note">
          Showing first {items.length} (results truncated by the server).
        </p>
      ) : null}
      {items.length === 0 ? (
        <p className="empty-note">No identity providers configured.</p>
      ) : (
        <table className="data-table" aria-label="Providers">
          <thead>
            <tr>
              <th>Label</th>
              <th>Kind</th>
              <th>Status</th>
              <th>Last login</th>
              <th>Group mappings</th>
              <th>Secret rotation</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => {
              const envManaged = item.managed_by === "environment";
              const busy = busyId === item.provider_config_id;
              const count = mappingCounts.get(item.provider_config_id);
              return (
                <tr key={item.provider_config_id}>
                  <td>
                    {deriveProviderLabel(item)}
                    {envManaged ? (
                      <>
                        {" "}
                        <Badge tone="violet">env-managed</Badge>
                      </>
                    ) : null}
                  </td>
                  <td>{kindLabel(item.provider_kind)}</td>
                  <td>{statusBadge(item.status)}</td>
                  <td>—</td>
                  <td>{count !== undefined ? count : "—"}</td>
                  <td>{fmt(item.updated_at)}</td>
                  <td>
                    <div className="row wrap" style={{ gap: 6 }}>
                      <button
                        type="button"
                        className="btn-ghost"
                        disabled={busy || !client}
                        onClick={() => void onTest(item)}
                      >
                        {busy ? "Working…" : "Test"}
                      </button>
                      <button
                        type="button"
                        className="btn-ghost"
                        disabled={busy || envManaged}
                        title={
                          envManaged
                            ? "Defined by deployment — edit in your IaC, not here."
                            : undefined
                        }
                        onClick={() => setDrawer({ mode: "edit", item })}
                      >
                        Edit
                      </button>
                      {item.status === "active" ? (
                        <button
                          type="button"
                          className="btn-ghost"
                          disabled={busy || envManaged}
                          title={
                            envManaged
                              ? "Defined by deployment — edit in your IaC, not here."
                              : undefined
                          }
                          onClick={() => void onDisable(item)}
                        >
                          Disable
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      {drawer && client ? (
        <Suspense fallback={null}>
          <ProviderConfigDrawer
            client={client}
            baseUrl={baseUrl ?? ""}
            existing={drawer.mode === "edit" ? drawer.item : undefined}
            onClose={() => setDrawer(null)}
            onSaved={() => setRefreshKey((k) => k + 1)}
          />
        </Suspense>
      ) : null}
    </Panel>
  );
}
