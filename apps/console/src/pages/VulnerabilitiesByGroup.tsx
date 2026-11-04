// pages/VulnerabilitiesByGroup.tsx
// "By service" view: one grouped read of impact findings
// (GET /api/v0/supply-chain/impact/inventory) so an operator sees which
// services, workloads, or environments carry the most impact without paging
// through every finding. Each group links into the finding list filtered to it.
import { Link } from "react-router";

import { useImpactInventory } from "./useImpactInventory";
import type { EshuApiClient } from "../api/client";
import {
  inventoryDimensionNoun,
  inventoryMultiGroupNote,
  sortInventoryBuckets,
  type InventoryBucket,
  type InventoryCompleteness,
  type InventoryDimension,
  type InventoryProfile,
} from "../api/eshuConsoleImpactInventory";
import { Badge } from "../components/atoms";

const GROUP_OPTIONS: readonly { readonly value: InventoryDimension; readonly label: string }[] = [
  { value: "service_id", label: "Service" },
  { value: "workload_id", label: "Workload" },
  { value: "environment", label: "Environment" },
];

const COMPLETENESS: Readonly<
  Record<InventoryCompleteness, { readonly label: string; readonly tone: "teal" | "warn" | "crit" }>
> = {
  complete: { label: "Complete", tone: "teal" },
  partial: { label: "Partial", tone: "warn" },
  unattributed: { label: "Not attributed", tone: "crit" },
  read_model_unavailable: { label: "Read model unavailable", tone: "crit" },
};

const STATUS_LABEL: Readonly<Record<string, string>> = {
  affected_exact: "exact",
  affected_derived: "derived",
  possibly_affected: "possibly affected",
};

const SEVERITY_ORDER = ["critical", "high", "medium", "low", "info", "unknown"];

function unattributedName(dimension: InventoryDimension): string {
  return dimension === "environment"
    ? "No current deploy evidence"
    : `No ${inventoryDimensionNoun(dimension)} attributed`;
}

function unattributedExplanation(dimension: InventoryDimension): string {
  const noun = inventoryDimensionNoun(dimension);
  return dimension === "environment"
    ? "These findings have no current deploy evidence, so they cannot be opened as an environment filter."
    : `These findings have no attributed ${noun}, so they cannot be opened as a ${noun} filter. This group is not a safe group.`;
}

function statusEntries(counts: Readonly<Record<string, number>>): readonly [string, number][] {
  return Object.entries(counts).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

function severityEntries(counts: Readonly<Record<string, number>>): readonly [string, number][] {
  const rank = (name: string): number => {
    const index = SEVERITY_ORDER.indexOf(name);
    return index === -1 ? SEVERITY_ORDER.length : index;
  };
  return Object.entries(counts).sort((a, b) => rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0]));
}

function filterHref(
  dimension: InventoryDimension,
  value: string,
  profile: InventoryProfile,
): string {
  const base = `/vulnerabilities?${dimension}=${encodeURIComponent(value)}`;
  return profile === "comprehensive" ? `${base}&profile=comprehensive` : base;
}

function CountChips({
  entries,
  label,
}: {
  readonly entries: readonly [string, number][];
  readonly label: (name: string) => string;
}): React.JSX.Element {
  if (entries.length === 0) return <span className="t-mut">—</span>;
  return (
    <span className="row" style={{ gap: 4, flexWrap: "wrap" }}>
      {entries.map(([name, count]) => (
        <Badge tone="neutral" key={name}>
          {`${label(name)} ${count}`}
        </Badge>
      ))}
    </span>
  );
}

function BucketRow({
  bucket,
  dimension,
  profile,
}: {
  readonly bucket: InventoryBucket;
  readonly dimension: InventoryDimension;
  readonly profile: InventoryProfile;
}): React.JSX.Element {
  const completeness = COMPLETENESS[bucket.completeness];
  return (
    <tr
      data-completeness={bucket.completeness}
      data-group-value={bucket.value}
      data-unattributed={bucket.unattributed ? "true" : "false"}
    >
      <td>
        {bucket.unattributed ? (
          <>
            <strong>{unattributedName(dimension)}</strong>
            <small className="t-mut" style={{ display: "block" }}>
              {unattributedExplanation(dimension)}
            </small>
          </>
        ) : (
          <Link className="t-name link-btn mono" to={filterHref(dimension, bucket.value, profile)}>
            {bucket.value}
          </Link>
        )}
      </td>
      <td className="mono">{bucket.count}</td>
      <td>
        <CountChips
          entries={statusEntries(bucket.byImpactStatus)}
          label={(name) => STATUS_LABEL[name] ?? name.replace(/_/g, " ")}
        />
      </td>
      <td>
        <CountChips entries={severityEntries(bucket.bySeverity)} label={(name) => name} />
      </td>
      <td className="mono">{bucket.maxPriorityScore ?? "—"}</td>
      <td>
        <Badge tone={completeness.tone}>{completeness.label}</Badge>
        {bucket.missingEvidence.length > 0 ? (
          <small className="t-mut" style={{ display: "block" }}>
            {`Missing: ${bucket.missingEvidence.join(", ")}`}
          </small>
        ) : null}
      </td>
    </tr>
  );
}

export function VulnerabilitiesByGroup({
  client,
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const inventory = useImpactInventory(client);
  const { groupBy, profile, page, loading, error, offsets } = inventory;
  const noun = inventoryDimensionNoun(groupBy);
  const buckets = page ? sortInventoryBuckets(page.buckets) : [];
  const note = page ? inventoryMultiGroupNote(page.buckets, page.distinctFindings, groupBy) : null;
  const echoed = page?.detectionProfile ?? "";
  return (
    <div aria-label="Grouped impact inventory">
      <p className="t-mut" style={{ fontSize: ".82rem", margin: "0 0 var(--gap)" }}>
        Impact by {noun} — <span className="mono">GET /api/v0/supply-chain/impact/inventory</span>.
      </p>
      {client ? null : (
        <p role="status">The grouped inventory needs the live API; no groups are shown here.</p>
      )}
      <div className="seg" role="group" aria-label="Group by">
        {GROUP_OPTIONS.map((option) => (
          <button
            key={option.value}
            type="button"
            className={groupBy === option.value ? "active" : ""}
            aria-pressed={groupBy === option.value}
            onClick={() => inventory.selectGroupBy(option.value)}
          >
            {option.label}
          </button>
        ))}
      </div>
      <p>
        <label>
          <input
            type="checkbox"
            checked={profile === "comprehensive"}
            onChange={(event) =>
              inventory.selectProfile(event.target.checked ? "comprehensive" : "precise")
            }
          />{" "}
          Include possibly affected
        </label>
        <small className="t-mut" style={{ display: "block" }}>
          Precise counts exact and derived impact only. Including possibly affected adds services
          whose only evidence is a possible match.
        </small>
      </p>
      {page ? (
        <p aria-live="polite">
          Detection profile: <strong>{echoed || "not reported"}</strong>
        </p>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      {loading ? <span role="status">Loading groups…</span> : null}
      {note ? <p role="note">{note}</p> : null}
      {page || !client ? (
        <div className="supply-chain-register-scroll" role="region" aria-label="Impact groups">
          <table className="tbl">
            <thead>
              <tr>
                <th>{GROUP_OPTIONS.find((option) => option.value === groupBy)?.label}</th>
                <th>Findings</th>
                <th>By status</th>
                <th>By severity</th>
                <th>Top priority</th>
                <th>Completeness</th>
              </tr>
            </thead>
            <tbody>
              {buckets.map((bucket) => (
                <BucketRow
                  key={`${bucket.unattributed ? "unattributed" : "group"}:${bucket.value}`}
                  bucket={bucket}
                  dimension={groupBy}
                  profile={profile}
                />
              ))}
              {page && buckets.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty">
                    No groups returned. Groups are built from findings, so a {noun} missing here has
                    no finding in this view; check the finding list for a specific {noun}.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      ) : null}
      {page ? (
        <div>
          <button
            type="button"
            disabled={loading || offsets.length < 2}
            onClick={() => inventory.previousPage()}
          >
            Previous groups page
          </button>
          <button
            type="button"
            disabled={loading || !page.truncated || page.nextOffset === null}
            onClick={() => inventory.nextPage()}
          >
            Next groups page
          </button>
        </div>
      ) : null}
    </div>
  );
}
