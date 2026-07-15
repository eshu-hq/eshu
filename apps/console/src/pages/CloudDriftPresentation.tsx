import { Link } from "react-router-dom";

import type { DriftSurfaceErrors } from "./cloudDriftLoad";
import type {
  IaCManagementExplanation,
  TerraformImportPlanCandidate,
  UnmanagedCloudResourceFinding,
} from "../api/cloudDrift";
import { FreshDot, Panel, TruthChip } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";

export function UnmanagedRow({
  candidate,
  finding,
  onExplain,
  pending,
}: {
  readonly candidate: TerraformImportPlanCandidate | undefined;
  readonly finding: UnmanagedCloudResourceFinding;
  readonly onExplain: (finding: UnmanagedCloudResourceFinding) => void;
  readonly pending: boolean;
}): React.JSX.Element {
  return (
    <tr>
      <td className="cell-stack">
        <span className="t-name">{finding.arn || finding.resourceId || finding.id}</span>
        <small>
          {finding.provider} {finding.accountId} {finding.region}
        </small>
      </td>
      <td>{finding.managementStatus || "-"}</td>
      <td className="t-mut">{listText(finding.missingEvidence) || "complete"}</td>
      <td className="cell-stack">
        {candidate ? (
          <>
            <span>{candidate.suggestedResourceAddress || candidate.status}</span>
            <small>
              {candidate.status}
              {candidate.refusalReasons.length > 0 ? `: ${listText(candidate.refusalReasons)}` : ""}
            </small>
            <Link to={importContextHref(candidate)}>Open import context</Link>
          </>
        ) : (
          <span className="t-mut">No candidate returned</span>
        )}
      </td>
      <td>{finding.safetyOutcome || "read_only"}</td>
      <td>
        <button
          className="btn-ghost"
          disabled={pending}
          type="button"
          onClick={() => onExplain(finding)}
        >
          {pending ? "Explaining..." : `Explain status for ${finding.arn}`}
        </button>
      </td>
    </tr>
  );
}

export function ManagementExplanationPanel({
  error,
  explanation,
}: {
  readonly error: string;
  readonly explanation: IaCManagementExplanation | null;
}): React.JSX.Element {
  return (
    <Panel
      title="Management explanation"
      sub={explanation?.arn || "Exact-resource evidence drilldown"}
    >
      {error ? <p className="empty">Failed to explain management status: {error}</p> : null}
      {explanation ? (
        <div className="evidence-card-list">
          <div className="evidence-card">
            <strong>{explanation.story}</strong>
            <span className="t-mut">Safety gate: {explanation.safetyOutcome || "read_only"}</span>
          </div>
          {explanation.evidenceGroups.map((group) => (
            <div className="evidence-card" key={group.layer}>
              <strong>
                {group.layer || "evidence"} · {group.count}
              </strong>
              {group.evidence.map((item) => (
                <span className="cell-stack mono t-mut" key={item.id}>
                  <span>{item.evidenceType}</span>
                  <small>
                    {item.key} · {item.value}
                  </small>
                </span>
              ))}
            </div>
          ))}
        </div>
      ) : (
        <p className="empty">
          Select an unmanaged resource to inspect its reducer evidence groups.
        </p>
      )}
    </Panel>
  );
}

export function TruthPair({
  truth,
}: {
  readonly truth: { readonly freshness: string; readonly level: string };
}): React.JSX.Element {
  return (
    <span className="panel-action-stack">
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness)} />
    </span>
  );
}

export function EmptyRow({
  cols,
  text,
}: {
  readonly cols: number;
  readonly text: string;
}): React.JSX.Element {
  return (
    <tr>
      <td className="empty" colSpan={cols}>
        {text}
      </td>
    </tr>
  );
}

function importContextHref(candidate: TerraformImportPlanCandidate): string {
  const params = new URLSearchParams();
  if (candidate.accountId) params.set("account_id", candidate.accountId);
  if (candidate.region) params.set("region", candidate.region);
  if (candidate.arn) params.set("arn", candidate.arn);
  return `/replatforming?${params.toString()}`;
}

export function listText(values: readonly string[]): string {
  return values.filter((value) => value.trim() !== "").join(", ");
}

export function pageSub(truncated: boolean | undefined): string {
  return truncated ? "more available" : "bounded page";
}

export function surfaceErrorMessage(errors: DriftSurfaceErrors): string {
  return [
    ["Multi-cloud drift", errors.multi],
    ["AWS drift", errors.aws],
    ["Unmanaged resources", errors.unmanaged],
    ["Import candidates", errors.importPlan],
  ]
    .filter((entry) => entry[1])
    .map((entry) => `${entry[0]}: ${entry[1]}`)
    .join("; ");
}
