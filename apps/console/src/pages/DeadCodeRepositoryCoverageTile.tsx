export function RepositoryCoverageTile({
  count,
  expanded,
  onToggle,
}: {
  readonly count: number;
  readonly expanded: boolean;
  readonly onToggle: () => void;
}): React.JSX.Element {
  return (
    <button
      aria-controls="dead-code-repository-breakdown"
      aria-expanded={expanded}
      aria-label={expanded ? "Hide repository breakdown" : "Show repository breakdown"}
      className="stat-tile dead-code-repository-tile"
      onClick={onToggle}
      type="button"
    >
      <div className="stat-tile-head">
        <span>Repositories represented</span>
      </div>
      <div className="stat-tile-body">
        <strong>{count}</strong>
      </div>
      <div className="stat-tile-sub">current result window · select for breakdown</div>
    </button>
  );
}
