import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import type { EshuTruth } from "../api/envelope";
import {
  loadReplatformingReview,
  type ReplatformingOwnership,
  type ReplatformingPlan,
  type ReplatformingPlanItem,
  type ReplatformingReview,
  type ReplatformingRollupBucket,
  type ReplatformingRollups,
  type ReplatformingSection,
  type ReplatformingSkippedSection,
} from "../api/replatforming";
import {
  loadReplatformingSelectors,
  type ReplatformingSelectorInventory,
} from "../api/replatformingSelectors";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import type { ConsoleModel } from "../console/types";
import { fmt, uiFresh, uiTruth } from "../console/types";
import {
  ReplatformingFilters,
  ReplatformingPagination,
  type ReplatformingFormState,
} from "./ReplatformingFilters";
import {
  classToken,
  formatLabel,
  formFromSearch,
  hasAnchor,
  inputFromForm,
  inventoryStatus,
  nextReviewOffset,
  optionalNumber,
  searchFromForm,
  shortStableId,
  statRows,
} from "./replatformingPageModel";
import "./replatformingPage.css";

const staticNonGoals = [
  "does not run Terraform or any migration",
  "does not import resources or mutate cloud state",
  "does not write user repositories",
] as const;

export function ReplatformingPage({
  client,
  model,
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [form, setForm] = useState<ReplatformingFormState>(() => formFromSearch(searchParams));
  const [inventory, setInventory] = useState<ReplatformingSelectorInventory | null>(null);
  const [inventoryError, setInventoryError] = useState("");
  const [inventoryLoading, setInventoryLoading] = useState(false);
  const [review, setReview] = useState<ReplatformingReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const requestSequence = useRef(0);
  const canLoad = model.source === "live" && client !== undefined;

  const runReview = useCallback(
    async (next: ReplatformingFormState) => {
      if (!client) return;
      if (!hasAnchor(next)) {
        requestSequence.current += 1;
        setReview(null);
        setBusy(false);
        setError("");
        return;
      }
      const requestID = requestSequence.current + 1;
      requestSequence.current = requestID;
      setBusy(true);
      setError("");
      try {
        const loaded = await loadReplatformingReview(client, inputFromForm(next));
        if (requestSequence.current === requestID) setReview(loaded);
      } catch (loadError) {
        if (requestSequence.current === requestID) {
          setReview(null);
          setError(
            loadError instanceof Error ? loadError.message : "failed to load replatforming plan",
          );
        }
      } finally {
        if (requestSequence.current === requestID) setBusy(false);
      }
    },
    [client],
  );

  useEffect(() => {
    if (!canLoad || !client) return;
    let active = true;
    setInventoryLoading(true);
    setInventoryError("");
    void loadReplatformingSelectors(client)
      .then((loaded) => {
        if (active) setInventory(loaded);
      })
      .catch((loadError: unknown) => {
        if (active) {
          setInventory(null);
          setInventoryError(
            loadError instanceof Error ? loadError.message : "failed to load selector inventory",
          );
        }
      })
      .finally(() => {
        if (active) setInventoryLoading(false);
      });
    return () => {
      active = false;
    };
  }, [canLoad, client]);

  useEffect(() => {
    const next = formFromSearch(searchParams);
    setForm(next);
    if (canLoad) void runReview(next);
  }, [canLoad, runReview, searchParams]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const params = searchFromForm(form);
    if (params.toString() === searchParams.toString()) {
      void runReview(form);
      return;
    }
    setSearchParams(params);
  }

  function setPageOffset(offset: number): void {
    const next = { ...form, offset: String(offset) };
    setForm(next);
    setSearchParams(searchFromForm(next));
  }

  const rollups = review?.rollups.status === "ready" ? review.rollups.data : null;
  const plan = review?.plan.status === "ready" ? review.plan.data : null;
  const ownership = review?.ownership.status === "ready" ? review.ownership.data : null;
  const stats = useMemo(() => statRows(rollups, plan, ownership), [rollups, plan, ownership]);
  const nonGoals = plan?.nonGoals.length ? plan.nonGoals : staticNonGoals;
  const statusMessage = inventoryStatus(inventory, inventoryLoading, review, busy);
  const currentOffset = optionalNumber(form.offset) ?? 0;
  const pageLimit = optionalNumber(form.limit) ?? 100;
  const nextOffset = nextReviewOffset(review, currentOffset, pageLimit);

  return (
    <div className="page replatforming-page" style={{ maxWidth: "none" }}>
      <div className="page-intro replatforming-intro">
        <h2>Replatforming plans</h2>
        <Badge tone="warn">read only</Badge>
      </div>

      <ReplatformingFilters
        canLoad={canLoad}
        form={form}
        inventory={inventory}
        onChange={setForm}
        onSubmit={submit}
      />
      {review !== null ? (
        <ReplatformingPagination
          busy={busy}
          canMoveNext={nextOffset !== null}
          canMovePrevious={currentOffset > 0}
          onNext={() => setPageOffset(nextOffset ?? currentOffset + pageLimit)}
          onPrevious={() => setPageOffset(Math.max(0, currentOffset - pageLimit))}
        />
      ) : null}

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {statusMessage ? <p className="inline-state">{statusMessage}</p> : null}
      {inventoryError ? (
        <p className="src-err">Selector inventory unavailable: {inventoryError}</p>
      ) : null}
      {error ? <p className="src-err">{error}</p> : null}

      <div className="replatforming-boundary mt">
        <strong>No-execution boundary</strong>
        {nonGoals.map((goal) => (
          <span key={goal}>{goal}</span>
        ))}
      </div>

      <div className="grid g-4 mt">
        {stats.map((stat) => (
          <StatTile
            color={stat.color}
            key={stat.label}
            label={stat.label}
            sub={stat.sub}
            value={stat.value}
          />
        ))}
      </div>

      <div className="replatforming-grid mt">
        <Panel title="Rollup readiness" sub="Bounded drift and readiness counts">
          <SectionStatus section={review?.rollups ?? null} />
          {rollups ? (
            <RollupSection rollups={rollups} />
          ) : (
            <SkippedState review={review} section="rollups" />
          )}
        </Panel>
        <Panel title="Migration packet" sub="Import candidates and refusal reasons">
          <SectionStatus section={review?.plan ?? null} />
          {plan ? <PlanSection plan={plan} /> : <SkippedState review={review} section="plan" />}
        </Panel>
      </div>

      <div className="mt">
        <Panel title="Ownership packets" sub="Candidates, missing evidence, and safety gates">
          <SectionStatus section={review?.ownership ?? null} />
          {ownership ? (
            <OwnershipSection ownership={ownership} />
          ) : (
            <SkippedState review={review} section="ownership" />
          )}
        </Panel>
      </div>
    </div>
  );
}

function SectionStatus<TData>({
  section,
}: {
  readonly section: ReplatformingSection<TData> | ReplatformingSkippedSection | null;
}): React.JSX.Element | null {
  if (section === null) return null;
  if (section.status === "skipped") return null;
  if (section.status === "unavailable") return <p className="src-err">{section.error}</p>;
  return <TruthSummary truth={section.truth} />;
}

function TruthSummary({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  if (truth === null) return <span className="t-mut">truth envelope unavailable</span>;
  return (
    <span className="replatforming-truth">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </span>
  );
}

function SkippedState({
  review,
  section,
}: {
  readonly review: ReplatformingReview | null;
  readonly section: "ownership" | "plan" | "rollups";
}): React.JSX.Element {
  const current = review?.[section];
  if (current?.status === "unavailable")
    return <p className="empty">This section is unavailable for the selected scope.</p>;
  if (current?.status === "skipped")
    return <p className="empty">Choose a bounded scope to query this section.</p>;
  return <p className="empty">Not queried yet.</p>;
}

function RollupSection({ rollups }: { readonly rollups: ReplatformingRollups }): React.JSX.Element {
  return (
    <div className="replatforming-section">
      <p>{rollups.story || "No rollup story returned."}</p>
      <PagingNote
        limit={rollups.limit}
        nextOffset={rollups.nextOffset}
        offset={rollups.offset}
        truncated={rollups.truncated}
      />
      <div className="replatforming-readiness">
        <Readiness label="Import ready" value={rollups.readinessTotals.importReady} />
        <Readiness label="Needs review" value={rollups.readinessTotals.needsReview} />
        <Readiness label="Refused" value={rollups.readinessTotals.refused} />
      </div>
      <BucketGroup title="Accounts" buckets={rollups.dimensions.account} />
      <BucketGroup title="Services" buckets={rollups.dimensions.service} />
      <BucketGroup title="Environments" buckets={rollups.dimensions.environment} />
    </div>
  );
}

function Readiness({
  label,
  value,
}: {
  readonly label: string;
  readonly value: number;
}): React.JSX.Element {
  return (
    <div>
      <strong>{fmt(value)}</strong>
      <span>{label}</span>
    </div>
  );
}

function BucketGroup({
  buckets,
  title,
}: {
  readonly buckets: readonly ReplatformingRollupBucket[];
  readonly title: string;
}): React.JSX.Element {
  return (
    <div className="replatforming-buckets">
      <strong>{title}</strong>
      {buckets.slice(0, 4).map((bucket) => (
        <span key={`${title}:${bucket.key}`}>
          {formatLabel(bucket.key)} <b>{fmt(bucket.total)}</b>
        </span>
      ))}
      {buckets.length === 0 ? <span>no buckets</span> : null}
    </div>
  );
}

function PlanSection({ plan }: { readonly plan: ReplatformingPlan }): React.JSX.Element {
  return (
    <div className="replatforming-section">
      <p>{plan.story || "No migration packet story returned."}</p>
      <PagingNote
        limit={plan.limit}
        nextOffset={plan.nextOffset}
        offset={plan.offset}
        truncated={plan.truncated}
      />
      <div className="replatforming-readiness">
        <Readiness label="Ready imports" value={plan.readyImportCount} />
        <Readiness label="Refused imports" value={plan.refusedImportCount} />
        <Readiness label="Packet items" value={plan.itemsCount} />
      </div>
      <div className="replatforming-table-wrap">
        <table className="tbl replatforming-table">
          <thead>
            <tr>
              <th>Resource</th>
              <th>Source state</th>
              <th>Import</th>
              <th>Wave</th>
            </tr>
          </thead>
          <tbody>
            {plan.items.map((item) => (
              <PlanRow item={item} key={item.itemId} />
            ))}
            {plan.items.length === 0 ? (
              <tr>
                <td className="empty" colSpan={4}>
                  No migration packet items returned.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PlanRow({ item }: { readonly item: ReplatformingPlanItem }): React.JSX.Element {
  const refusalReason = item.importCandidate.refusalReasons[0] ?? "";
  return (
    <tr>
      <td>
        <CellStack title={item.resourceType || item.itemId} sub={shortStableId(item.stableId)} />
      </td>
      <td>
        <StatusPill value={item.sourceState} />
      </td>
      <td>
        <span
          className={`replatforming-import replatforming-import-${classToken(item.importCandidate.status)}`}
        >
          {formatLabel(item.importCandidate.status)}
        </span>
        {refusalReason ? (
          <span className="replatforming-reason">{formatLabel(refusalReason)}</span>
        ) : null}
      </td>
      <td>
        <CellStack
          title={item.waveId || "unassigned"}
          sub={`Gate: ${formatLabel(item.safetyGate.outcome || "unspecified")}`}
        />
      </td>
    </tr>
  );
}

function OwnershipSection({
  ownership,
}: {
  readonly ownership: ReplatformingOwnership;
}): React.JSX.Element {
  return (
    <div className="replatforming-section">
      <p>{ownership.story || "No ownership packet story returned."}</p>
      <PagingNote
        limit={ownership.limit}
        nextOffset={ownership.nextOffset}
        offset={ownership.offset}
        truncated={ownership.truncated}
      />
      <div className="replatforming-readiness">
        <Readiness label="Packets" value={ownership.packetsCount} />
        <Readiness label="Ambiguous" value={ownership.ambiguousCount} />
        <Readiness label="Unattributed" value={ownership.unattributedCount} />
        <Readiness label="Rejected" value={ownership.rejectedCount} />
      </div>
      <div className="replatforming-packets">
        {ownership.packets.map((packet) => (
          <div key={packet.itemId}>
            <CellStack title={packet.itemId} sub={shortStableId(packet.stableId)} />
            <div className="replatforming-candidates">
              {packet.ownerCandidates.map((candidate) => (
                <span key={`${packet.itemId}:${candidate.kind}:${candidate.value}`}>
                  <small>{formatLabel(candidate.kind)}</small>
                  {candidate.value}
                </span>
              ))}
              {packet.ownerCandidates.length === 0 ? <span>no candidates</span> : null}
            </div>
            <div className="replatforming-gaps">
              {packet.missingEvidence.map((gap) => (
                <span key={gap}>{formatLabel(gap)}</span>
              ))}
              {packet.missingEvidence.length === 0 ? <span>no missing evidence</span> : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function StatusPill({ value }: { readonly value: string }): React.JSX.Element {
  return (
    <span className={`replatforming-state replatforming-state-${classToken(value)}`}>
      {formatLabel(value)}
    </span>
  );
}

function PagingNote({
  limit,
  nextOffset,
  offset,
  truncated,
}: {
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly truncated: boolean;
}): React.JSX.Element {
  const next = truncated
    ? nextOffset === null
      ? " More rows are available."
      : ` Next offset ${fmt(nextOffset)}.`
    : "";
  return (
    <p className="replatforming-page-note">
      Showing up to {fmt(limit)} rows from offset {fmt(offset)}.{next}
    </p>
  );
}

function CellStack({
  sub,
  title,
}: {
  readonly sub: string;
  readonly title: string;
}): React.JSX.Element {
  return (
    <span className="cell-stack">
      <span className="t-name">{title}</span>
      {sub ? <small className="mono">{sub}</small> : null}
    </span>
  );
}
