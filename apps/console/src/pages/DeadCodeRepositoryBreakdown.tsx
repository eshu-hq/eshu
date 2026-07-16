import { Link } from "react-router-dom";

import {
  deadCodeRepositoryHref,
  locFromFinding,
  type DeadCodeRepositoryGroup,
} from "./deadCodePresentation";
import { Panel } from "../components/atoms";
import { fmt } from "../console/types";

export function DeadCodeRepositoryBreakdown({
  currentCandidateKind,
  currentLanguage,
  groups,
  visible,
}: {
  readonly currentCandidateKind: string;
  readonly currentLanguage: string;
  readonly groups: readonly DeadCodeRepositoryGroup[];
  readonly visible: boolean;
}): React.JSX.Element {
  if (!visible) {
    return <section hidden id="dead-code-repository-breakdown" />;
  }
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
