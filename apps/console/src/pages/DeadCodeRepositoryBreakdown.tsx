import { Link } from "react-router-dom";

import {
  deadCodeRepositoryHref,
  locFromFinding,
  type DeadCodeRepositoryGroup,
} from "./deadCodePresentation";
import { Panel } from "../components/atoms";
import { fmt } from "../console/types";

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

export function DeadCodeRepositoryBreakdown({
  currentCandidateKind,
  currentLanguage,
  groups,
}: {
  readonly currentCandidateKind: string;
  readonly currentLanguage: string;
  readonly groups: readonly DeadCodeRepositoryGroup[];
}): React.JSX.Element {
  return (
    <section
      aria-label="Repository breakdown"
      className="dead-code-repository-breakdown mt"
      id="dead-code-repository-breakdown"
    >
      <Panel
        className="flush"
        title="Repositories in the current result window"
        sub="Counts and estimated LOC below describe only the candidates returned by this bounded response."
      >
        <div className="table-scroll">
          <table className="tbl wide">
            <thead>
              <tr>
                <th>Repository</th>
                <th>Canonical identifier</th>
                <th>Candidates shown</th>
                <th>Estimated LOC shown</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((group) => {
                const loc = group.rows.reduce((sum, finding) => sum + locFromFinding(finding), 0);
                return (
                  <tr className="cloud-row" key={group.key}>
                    <td>{group.repository}</td>
                    <td className="mono t-mut">
                      {group.repositoryId ?? "Canonical identifier unavailable"}
                    </td>
                    <td>{group.rows.length}</td>
                    <td>{fmt(loc)} LOC</td>
                    <td>
                      {group.repositoryId ? (
                        <Link
                          className="btn-ghost"
                          to={deadCodeRepositoryHref(
                            group.repositoryId,
                            currentLanguage,
                            currentCandidateKind,
                          )}
                        >
                          View candidates
                        </Link>
                      ) : (
                        <span className="t-mut">Scoped view unavailable</span>
                      )}
                    </td>
                  </tr>
                );
              })}
              {groups.length === 0 ? (
                <tr>
                  <td className="empty" colSpan={5}>
                    No repositories are represented in this result window.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </Panel>
    </section>
  );
}
