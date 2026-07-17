import { Link } from "react-router-dom";

import {
  classificationFromFinding,
  codeGraphHref,
  kindFromFinding,
  locationFromFinding,
  locFromFinding,
  sourceHref,
  symbolFromFinding,
  type DeadCodeRepositoryGroup,
} from "./deadCodePresentation";
import { Badge, TruthChip } from "../components/atoms";
import { fmt, uiTruth } from "../console/types";

export function DeadCodeTableRows({
  groups,
}: {
  readonly groups: readonly DeadCodeRepositoryGroup[];
}): React.JSX.Element {
  return (
    <>
      {groups.map((group) => (
        <DeadCodeGroup key={group.key} group={group} />
      ))}
    </>
  );
}

function DeadCodeGroup({ group }: { readonly group: DeadCodeRepositoryGroup }): React.JSX.Element {
  const loc = group.rows.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  return (
    <>
      <tr className="group-row">
        <td colSpan={9}>
          <span className="group-label" style={{ color: "var(--ember)" }}>
            {group.repository}
          </span>
          <span className="group-meta">
            {group.rows.length} dead · {fmt(loc)} LOC
          </span>
        </td>
      </tr>
      {group.rows.map((finding) => {
        const href = sourceHref(finding);
        return (
          <tr key={finding.id} className="cloud-row">
            <td className="cell-stack">
              <span className="mono" style={{ color: "var(--bone)", fontWeight: 600 }}>
                {symbolFromFinding(finding)}
              </span>
              <small>{finding.title}</small>
            </td>
            <td>
              <Badge tone="neutral">{kindFromFinding(finding)}</Badge>
            </td>
            <td className="dead-code-row-language">{finding.language ?? "language unavailable"}</td>
            <td className="t-mut mono" style={{ fontSize: ".74rem" }}>
              {href ? (
                <Link className="mono" to={href}>
                  {locationFromFinding(finding)}
                </Link>
              ) : (
                locationFromFinding(finding)
              )}
            </td>
            <td>
              <span className="mono" style={{ color: "var(--crit)", fontWeight: 700 }}>
                0
              </span>
            </td>
            <td className="t-mut mono" style={{ fontSize: ".78rem" }}>
              {locFromFinding(finding) || "—"}
            </td>
            <td>
              <TruthChip level={uiTruth(finding.truth)} />
            </td>
            <td className="t-mut" style={{ fontSize: ".78rem", maxWidth: 360 }}>
              {classificationFromFinding(finding) || "candidate"}
            </td>
            <td>
              <Link className="btn-ghost" to={codeGraphHref(finding)}>
                Open graph
              </Link>
            </td>
          </tr>
        );
      })}
    </>
  );
}
