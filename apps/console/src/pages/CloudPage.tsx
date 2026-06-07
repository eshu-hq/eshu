// pages/CloudPage.tsx
// Cloud inventory browser (#1643). Browses cloud-provider resources from the
// bounded, keyset-paged GET /api/v0/cloud/resources endpoint. The graph holds
// ~17k CloudResource nodes, so this page never loads them all at once: it pages
// forward with the server's next_cursor and lets the operator narrow the set with
// provider/type/region/account filters. Every value is live; nothing is invented.
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { loadCloudResources } from "../api/cloudResources";
import type {
  CloudResourceCursor,
  CloudResourcePage,
  CloudResourceQuery,
  CloudResourceRow
} from "../api/cloudResources";
import { Panel, TruthChip, FreshDot } from "../components/atoms";
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
    cursor: cursor ?? undefined
  };
}

export function CloudPage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const [page, setPage] = useState<CloudResourcePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS);
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS);
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
    [client]
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
  const sub = page ? (busy ? "loading…" : "live") : busy ? "loading…" : "—";

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Cloud</h2>
        <p>
          Cloud-provider resource inventory from{" "}
          <span className="mono">GET /api/v0/cloud/resources</span>. Bounded,
          keyset-paged over the authoritative <span className="mono">CloudResource</span> graph.
        </p>
      </div>

      <Panel
        className="flush"
        title={`Cloud resources · page ${pageNumber}`}
        sub={sub}
        action={
          page ? (
            <div className="row" style={{ gap: 8, alignItems: "center" }}>
              <TruthChip level={uiTruth(page.truth.level)} />
              <FreshDot state={uiFresh(page.truth.freshness)} />
            </div>
          ) : null
        }
      >
        <form
          className="row"
          style={{ gap: 8, flexWrap: "wrap", padding: "10px 12px" }}
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
        </form>

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
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <CloudRow key={r.id} row={r} />
              ))}
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={7} className="empty">
                    {err
                      ? `Failed to load: ${err}`
                      : "No cloud resources match this scope."}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        )}

        <div
          className="row"
          style={{ gap: 10, alignItems: "center", padding: "10px 12px", justifyContent: "space-between" }}
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
      </Panel>
    </div>
  );
}

// CloudRow renders one resource and links its name into Graph Explorer so the
// operator can pivot from inventory into the relationship graph. The label falls
// back to id when the node has no name; the explorer query uses the canonical id.
function CloudRow({ row }: { readonly row: CloudResourceRow }): React.JSX.Element {
  const label = row.name || row.id;
  return (
    <tr>
      <td className="mono" style={{ fontSize: ".78rem" }}>{row.resourceType || "—"}</td>
      <td className="t-name" title={row.arn || row.id}>
        <Link to={`/explorer?q=${encodeURIComponent(row.id)}`}>{label}</Link>
      </td>
      <td className="t-mut">{row.region || "—"}</td>
      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{row.accountId || "—"}</td>
      <td className="t-mut">{row.provider || "—"}</td>
      <td className="t-mut">{row.state || "—"}</td>
      <td className="t-mut mono" style={{ fontSize: ".7rem", maxWidth: 220, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={row.id}>
        {row.serviceName || ""}
      </td>
    </tr>
  );
}
