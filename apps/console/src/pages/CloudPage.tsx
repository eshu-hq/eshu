// pages/CloudPage.tsx
// Cloud inventory browser (#1643). Browses cloud-provider resources from the
// bounded, keyset-paged GET /api/v0/cloud/resources endpoint. The graph holds
// ~17k CloudResource nodes, so this page never loads them all at once: it pages
// forward with the server's next_cursor and lets the operator narrow the set with
// provider/type/region/account filters. Private mode values are live API data;
// demo values come only from the explicit prospect fixture source.
import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import { Link } from "react-router-dom";

import { CloudInventoryPanel } from "./CloudInventoryPanel";
import type { EshuApiClient } from "../api/client";
import { loadCloudResources } from "../api/cloudResources";
import type {
  CloudResourceCursor,
  CloudResourcePage,
  CloudResourceQuery,
  CloudResourceRow,
} from "../api/cloudResources";
import { Panel, TruthChip, FreshDot, StatTile, Badge } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";
import { uiTruth, uiFresh } from "../console/types";

const PAGE_LIMIT = 50;

// Filters mirrors the bounded server-side filters the endpoint accepts. These are
// applied by the API, not in the browser, so they compose with keyset paging.
interface Filters {
  readonly provider: string;
  readonly resourceType: string;
  readonly region: string;
  readonly accountId: string;
}

const EMPTY_FILTERS: Filters = { provider: "", resourceType: "", region: "", accountId: "" };

function queryFor(filters: Filters, cursor: CloudResourceCursor | null): CloudResourceQuery {
  return {
    limit: PAGE_LIMIT,
    provider: filters.provider.trim() || undefined,
    resourceType: filters.resourceType.trim() || undefined,
    region: filters.region.trim() || undefined,
    accountId: filters.accountId.trim() || undefined,
    cursor: cursor ?? undefined,
  };
}

export function CloudPage({
  client,
  sourceLabel = "live",
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [page, setPage] = useState<CloudResourcePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS);
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS);
  const [view, setView] = useState<"network" | "table">("network");
  const [networkAccount, setNetworkAccount] = useState("");
  // The cursor stack lets the operator page backward: each entry is the cursor
  // used to load the page now on screen (null = first page).
  const [stack, setStack] = useState<readonly (CloudResourceCursor | null)[]>([null]);

  const fetchPage = useCallback(
    (filters: Filters, cursor: CloudResourceCursor | null) => {
      if (!client) {
        setPage(null);
        return () => undefined;
      }
      let cancelled = false;
      setBusy(true);
      setErr("");
      void loadCloudResources(client, queryFor(filters, cursor))
        .then((result) => {
          if (!cancelled) {
            setPage(result);
            setBusy(false);
          }
        })
        .catch((e) => {
          if (!cancelled) {
            setPage(null);
            setBusy(false);
            setErr(e instanceof Error ? e.message : "failed to load cloud resources");
          }
        });
      return () => {
        cancelled = true;
      };
    },
    [client],
  );

  // Load the first page on connect, and reload it whenever applied filters change.
  useEffect(() => fetchPage(applied, null), [fetchPage, applied]);

  function onSearch(): void {
    setStack([null]);
    setApplied(draft);
  }

  function onReset(): void {
    setDraft(EMPTY_FILTERS);
    setStack([null]);
    setApplied(EMPTY_FILTERS);
  }

  function onNext(): void {
    if (!page?.nextCursor) return;
    const next = page.nextCursor;
    setStack((s) => [...s, next]);
    fetchPage(applied, next);
  }

  function onPrev(): void {
    if (stack.length <= 1) return;
    const nextStack = stack.slice(0, -1);
    setStack(nextStack);
    fetchPage(applied, nextStack[nextStack.length - 1]);
  }

  const rows = page?.rows ?? [];
  const pageNumber = stack.length;
  const sub = page ? (busy ? "loading…" : sourceLabel) : busy ? "loading…" : "—";
  const families = useMemo(() => familyRollups(rows), [rows]);
  const accounts = useMemo(() => accountRollups(rows), [rows]);
  const selectedAccount = networkAccount || accounts[0]?.id || "";
  const network = useMemo(() => cloudNetworkGraph(rows, selectedAccount), [rows, selectedAccount]);

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Cloud</h2>
        <p>
          Cloud-provider resource inventory from{" "}
          <span className="mono">GET /api/v0/cloud/resources</span>. Network and table views are
          derived from the current bounded page of authoritative{" "}
          <span className="mono">CloudResource</span> graph rows.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile
          label="Cloud resources"
          value={page?.count ?? rows.length}
          color="var(--blue)"
          sub={`page ${pageNumber}${page?.truncated ? " · more available" : ""}`}
        />
        <StatTile
          label="Accounts"
          value={accounts.length}
          color="var(--ember)"
          sub="on current page"
        />
        <StatTile
          label="Resource families"
          value={families.length}
          color="var(--teal)"
          sub="typed from resource_type"
        />
        <StatTile
          label="Endpoint"
          value={sourceLabel}
          color="var(--violet)"
          sub="/api/v0/cloud/resources"
        />
      </div>

      <div
        className="grid mt"
        style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: "var(--gap)" }}
      >
        <Panel title="Resources by family" sub="Current bounded page">
          <div className="kv-list">
            {families.map((family) => (
              <div className="kv" key={family.key}>
                <span>
                  <i
                    style={{
                      display: "inline-block",
                      width: 8,
                      height: 8,
                      borderRadius: 2,
                      background: family.color,
                      marginRight: 7,
                    }}
                  />
                  {family.label}
                </span>
                <strong>{family.count}</strong>
              </div>
            ))}
            {families.length === 0 ? <p className="empty">No family rollup yet.</p> : null}
          </div>
        </Panel>
        <Panel title="Accounts" sub="Provider · region · resources">
          <div className="acct-list">
            {accounts.map((account) => (
              <button
                key={account.id}
                type="button"
                className="acct-row"
                onClick={() => setNetworkAccount(account.id)}
              >
                <span
                  className="acct-prov"
                  style={{ "--pc": providerColor(account.provider) } as CSSProperties}
                >
                  <i />
                  {account.provider || "provider"}
                </span>
                <span className="cell-stack" style={{ flex: 1, minWidth: 0 }}>
                  <span className="t-name" style={{ fontSize: ".84rem" }}>
                    {account.id}
                  </span>
                  <small className="mono">{account.region}</small>
                </span>
                <span className="mono t-mut" style={{ fontSize: ".78rem" }}>
                  {account.count}
                </span>
              </button>
            ))}
            {accounts.length === 0 ? <p className="empty">No accounts on this page.</p> : null}
          </div>
        </Panel>
      </div>

      <CloudInventoryPanel
        accountId={applied.accountId}
        client={client}
        provider={applied.provider}
      />

      <div
        className="row mt"
        style={{ justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}
      >
        <div className="seg" role="group" aria-label="Cloud view">
          <button className={view === "network" ? "active" : ""} onClick={() => setView("network")}>
            Network
          </button>
          <button className={view === "table" ? "active" : ""} onClick={() => setView("table")}>
            Table
          </button>
        </div>
        {view === "network" && accounts.length > 0 ? (
          <div className="seg branch-seg">
            {accounts.map((account) => (
              <button
                key={account.id}
                className={selectedAccount === account.id ? "active" : ""}
                onClick={() => setNetworkAccount(account.id)}
              >
                {account.id}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <form
        className="row mt"
        style={{ gap: 8, flexWrap: "wrap", alignItems: "center" }}
        onSubmit={(e) => {
          e.preventDefault();
          onSearch();
        }}
      >
        <input
          className="popover-input mono"
          style={{ minWidth: 120 }}
          placeholder="provider (aws)"
          aria-label="provider filter"
          value={draft.provider}
          onChange={(e) => setDraft((f) => ({ ...f, provider: e.target.value }))}
        />
        <input
          className="popover-input mono"
          style={{ minWidth: 160 }}
          placeholder="resource_type (aws_iam_role)"
          aria-label="resource type filter"
          value={draft.resourceType}
          onChange={(e) => setDraft((f) => ({ ...f, resourceType: e.target.value }))}
        />
        <input
          className="popover-input mono"
          style={{ minWidth: 120 }}
          placeholder="region (us-east-1)"
          aria-label="region filter"
          value={draft.region}
          onChange={(e) => setDraft((f) => ({ ...f, region: e.target.value }))}
        />
        <input
          className="popover-input mono"
          style={{ minWidth: 140 }}
          placeholder="account_id"
          aria-label="account id filter"
          value={draft.accountId}
          onChange={(e) => setDraft((f) => ({ ...f, accountId: e.target.value }))}
        />
        <button type="submit" className="btn-ghost active">
          Apply
        </button>
        <button type="button" className="btn-ghost" onClick={onReset}>
          Reset
        </button>
        <span style={{ flex: 1 }} />
        {page ? (
          <>
            <TruthChip level={uiTruth(page.truth.level)} />
            <FreshDot state={uiFresh(page.truth.freshness)} />
          </>
        ) : null}
      </form>

      {view === "network" ? (
        <Panel
          className="flush mt"
          title="Network topology"
          sub={`Account → region → family → resources · ${selectedAccount || "no account selected"}`}
        >
          {page === null && busy ? (
            <div className="conn-state" style={{ padding: 40 }}>
              <div className="conn-spinner" aria-hidden />
              <p>Loading cloud resources…</p>
            </div>
          ) : network.nodes.length > 0 ? (
            <GraphCanvas graph={network} layout="layered" height={520} />
          ) : (
            <p className="empty">
              {err ? `Failed to load: ${err}` : "No cloud resources match this scope."}
            </p>
          )}
        </Panel>
      ) : (
        <Panel
          className="flush"
          title={`Cloud resources · page ${pageNumber}`}
          sub={`Grouped by family · ${sub}`}
        >
          {page === null && busy ? (
            <div className="conn-state" style={{ padding: 40 }}>
              <div className="conn-spinner" aria-hidden />
              <p>Loading cloud resources…</p>
            </div>
          ) : (
            <table className="tbl">
              <thead>
                <tr>
                  <th>Type</th>
                  <th>Name / ID</th>
                  <th>Region</th>
                  <th>Account</th>
                  <th>Provider</th>
                  <th>State</th>
                  <th>Family</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {families.map((family) => (
                  <CloudFamilyRows
                    key={family.key}
                    family={family}
                    rows={rows.filter((row) => familyFor(row).key === family.key)}
                  />
                ))}
                {rows.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="empty">
                      {err ? `Failed to load: ${err}` : "No cloud resources match this scope."}
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          )}
        </Panel>
      )}

      <div
        className="row"
        style={{
          gap: 10,
          alignItems: "center",
          padding: "10px 0",
          justifyContent: "space-between",
        }}
      >
        <span className="t-mut" style={{ fontSize: ".76rem" }}>
          {page ? `${page.count} on this page${page.truncated ? " · more available" : ""}` : "—"}
        </span>
        <div className="row" style={{ gap: 8 }}>
          <button
            type="button"
            className="btn-ghost"
            disabled={busy || pageNumber <= 1}
            onClick={onPrev}
          >
            ← Prev
          </button>
          <button
            type="button"
            className="btn-ghost active"
            disabled={busy || !page?.nextCursor}
            onClick={onNext}
          >
            Next →
          </button>
        </div>
      </div>
    </div>
  );
}

// CloudRow renders one resource and links its name into Graph Explorer so the
// operator can pivot from inventory into the relationship graph. The label falls
// back to id when the node has no name; the explorer query uses the canonical id.
function CloudRow({ row }: { readonly row: CloudResourceRow }): React.JSX.Element {
  const label = row.name || row.id;
  const family = familyFor(row);
  return (
    <tr>
      <td className="mono" style={{ fontSize: ".78rem" }}>
        {row.resourceType || "—"}
      </td>
      <td className="t-name" title={row.arn || row.id}>
        <Link to={`/explorer?q=${encodeURIComponent(row.id)}`}>{label}</Link>
      </td>
      <td className="t-mut">{row.region || "—"}</td>
      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>
        {row.accountId || "—"}
      </td>
      <td className="t-mut">{row.provider || "—"}</td>
      <td className="t-mut">{row.state || "—"}</td>
      <td>
        <Badge tone="neutral">{family.label}</Badge>
      </td>
      <td
        className="t-mut mono"
        style={{
          fontSize: ".7rem",
          maxWidth: 220,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
        title={row.id}
      >
        {row.serviceName || ""}
      </td>
    </tr>
  );
}

function CloudFamilyRows({
  family,
  rows,
}: {
  readonly family: CloudFamily;
  readonly rows: readonly CloudResourceRow[];
}): React.JSX.Element {
  return (
    <>
      <tr className="group-row">
        <td colSpan={8}>
          <span className="group-label" style={{ color: family.color }}>
            {family.label}
          </span>
          <span className="group-meta">
            {rows.length} {rows.length === 1 ? "resource" : "resources"}
          </span>
        </td>
      </tr>
      {rows.map((row) => (
        <CloudRow key={row.id} row={row} />
      ))}
    </>
  );
}

interface CloudFamily {
  readonly key: string;
  readonly label: string;
  readonly color: string;
  readonly count: number;
}

interface AccountRollup {
  readonly id: string;
  readonly provider: string;
  readonly region: string;
  readonly count: number;
}

function familyRollups(rows: readonly CloudResourceRow[]): readonly CloudFamily[] {
  const counts = new Map<string, CloudFamily>();
  for (const row of rows) {
    const family = familyFor(row);
    counts.set(family.key, { ...family, count: (counts.get(family.key)?.count ?? 0) + 1 });
  }
  return [...counts.values()].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
}

function accountRollups(rows: readonly CloudResourceRow[]): readonly AccountRollup[] {
  const accounts = new Map<string, AccountRollup>();
  for (const row of rows) {
    const id = row.accountId || "unknown";
    const current = accounts.get(id);
    accounts.set(id, {
      id,
      provider: current?.provider || row.provider,
      region: current?.region || row.region,
      count: (current?.count ?? 0) + 1,
    });
  }
  return [...accounts.values()].sort((a, b) => b.count - a.count || a.id.localeCompare(b.id));
}

function familyFor(row: CloudResourceRow): CloudFamily {
  const type = row.resourceType.toLowerCase();
  if (type.includes("iam") || type.includes("role") || type.includes("policy"))
    return { key: "identity", label: "Identity & access", color: "#ff9d2e", count: 0 };
  if (
    type.includes("s3") ||
    type.includes("rds") ||
    type.includes("dynamo") ||
    type.includes("elasticache") ||
    type.includes("opensearch")
  )
    return { key: "storage", label: "Storage", color: "#f59e0b", count: 0 };
  if (
    type.includes("vpc") ||
    type.includes("subnet") ||
    type.includes("security_group") ||
    type.includes("gateway") ||
    type.includes("route")
  )
    return { key: "network", label: "Network", color: "#4f8cff", count: 0 };
  if (
    type.includes("eks") ||
    type.includes("ecs") ||
    type.includes("lambda") ||
    type.includes("apigateway")
  )
    return { key: "compute", label: "Compute & runtime", color: "#14b8a6", count: 0 };
  if (
    type.includes("cloudwatch") ||
    type.includes("grafana") ||
    type.includes("log") ||
    type.includes("alarm")
  )
    return { key: "observability", label: "Observability", color: "#22c55e", count: 0 };
  return { key: "other", label: "Other", color: "#8b5cf6", count: 0 };
}

function providerColor(provider: string): string {
  if (provider === "aws") return "#ff9d2e";
  if (provider === "gcp") return "#22d3ee";
  if (provider === "azure") return "#4f8cff";
  return "#9aa4af";
}

function cloudNetworkGraph(rows: readonly CloudResourceRow[], accountId: string): GraphModel {
  const scoped = rows.filter((row) => (row.accountId || "unknown") === accountId);
  if (accountId === "" || scoped.length === 0) return { nodes: [], edges: [] };
  const nodes = new Map<string, GraphNode>();
  const edges: GraphEdge[] = [];
  nodes.set(`account:${accountId}`, {
    id: `account:${accountId}`,
    label: accountId,
    kind: "aws",
    sub: "account",
    col: 0,
    hero: true,
  });
  for (const row of scoped) {
    const regionId = `region:${row.region || "unknown"}`;
    const family = familyFor(row);
    const familyId = `family:${family.key}`;
    nodes.set(regionId, {
      id: regionId,
      label: row.region || "unknown",
      kind: "env",
      sub: row.provider || "provider",
      col: 1,
    });
    nodes.set(familyId, {
      id: familyId,
      label: family.label,
      kind: kindForFamily(family),
      sub: "resource family",
      col: 2,
    });
    nodes.set(row.id, {
      id: row.id,
      label: row.name || row.resourceType || row.id,
      kind: kindForFamily(family),
      sub: row.resourceType,
      col: 3,
    });
    edges.push({ s: `account:${accountId}`, t: regionId, verb: "CONTAINS", layer: "infra" });
    edges.push({ s: regionId, t: familyId, verb: "GROUPS", layer: "infra" });
    edges.push({ s: familyId, t: row.id, verb: "HAS_RESOURCE", layer: "infra" });
  }
  return { nodes: [...nodes.values()], edges };
}

function kindForFamily(family: CloudFamily): string {
  return family.key === "storage"
    ? "datastore"
    : family.key === "network"
      ? "workload"
      : family.key === "compute"
        ? "service"
        : "aws";
}
