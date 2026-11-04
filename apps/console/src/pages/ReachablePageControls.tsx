import type { useReachableFindingPage } from "./useReachableFindingPage";

const STATUS_LABEL = {
  affected_exact: "Exact",
  affected_derived: "Derived",
  possibly_affected: "Possibly affected",
} as const;

type Paging = ReturnType<typeof useReachableFindingPage>;

export function ReachablePageControls({
  paging,
  clientAvailable,
  rowsShown,
}: {
  readonly paging: Paging;
  readonly clientAvailable: boolean;
  readonly rowsShown: number;
}): React.JSX.Element {
  return (
    <div aria-label="Reachable findings pages">
      <div className="seg" role="group" aria-label="Impact status">
        <button
          type="button"
          className={paging.status === "affected_exact" ? "active" : ""}
          onClick={() => paging.selectStatus("affected_exact")}
        >
          Exact impact
        </button>
        <button
          type="button"
          className={paging.status === "affected_derived" ? "active" : ""}
          onClick={() => paging.selectStatus("affected_derived")}
        >
          Derived impact
        </button>
        {paging.filter ? (
          <button
            type="button"
            className={paging.status === "possibly_affected" ? "active" : ""}
            onClick={() => paging.selectStatus("possibly_affected")}
          >
            Possibly affected
          </button>
        ) : null}
      </div>
      <p className="t-mut" aria-live="polite">
        {STATUS_LABEL[paging.status]} findings, page {paging.index + 1}: {rowsShown} shown
        {paging.page?.truncated ? "; more pages available" : ""}. Counts and charts below cover this
        page only.
      </p>
      {paging.page?.detectionProfile ? (
        <p className="t-mut">
          Detection profile: <strong>{paging.page.detectionProfile}</strong>
        </p>
      ) : null}
      {paging.error ? <p role="alert">{paging.error}</p> : null}
      <button
        type="button"
        disabled={!clientAvailable || paging.loading || paging.index === 0}
        onClick={() => void paging.loadPage("previous")}
      >
        Previous findings page
      </button>
      <button
        type="button"
        disabled={!clientAvailable || paging.loading || !paging.page?.truncated}
        onClick={() => void paging.loadPage("next")}
      >
        Next findings page
      </button>
      {paging.loading ? <span role="status"> Loading findings page…</span> : null}
    </div>
  );
}

// FilteredListPending is the whole body of a filtered finding list whose own
// read has not produced a page: loading, an error, or no client. It never
// shows counts, a register, or "no affected service", because none of those
// are known yet.
export function FilteredListPending({
  paging,
  clientAvailable,
}: {
  readonly paging: Paging;
  readonly clientAvailable: boolean;
}): React.JSX.Element {
  return (
    <div>
      <ReachablePageControls paging={paging} clientAvailable={clientAvailable} rowsShown={0} />
      {clientAvailable ? null : (
        <p role="status">A filtered finding list needs the live API; no findings are shown.</p>
      )}
    </div>
  );
}
