import type { useReachableFindingPage } from "./useReachableFindingPage";

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
      </div>
      <p className="t-mut" aria-live="polite">
        {paging.status === "affected_exact" ? "Exact" : "Derived"} findings, page {paging.index + 1}
        : {rowsShown} shown
        {paging.page?.truncated ? "; more pages available" : ""}. Counts and charts below cover this
        page only.
      </p>
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
