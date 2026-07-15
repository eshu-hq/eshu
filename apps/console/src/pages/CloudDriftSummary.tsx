import type { DriftState, DriftSurfaceErrors } from "./cloudDriftLoad";
import { pageSub } from "./CloudDriftPresentation";
import {
  cloudDriftSurfaceLabel,
  cloudDriftSurfaceStatus,
  cloudDriftSurfaceValue,
} from "./CloudDriftSurfaceStatus";
import { StatTile } from "../components/atoms";

export function CloudDriftSummary({
  awsEnabled,
  multiEnabled,
  state,
  surfaceErrors,
}: {
  readonly awsEnabled: boolean;
  readonly multiEnabled: boolean;
  readonly state: DriftState;
  readonly surfaceErrors: DriftSurfaceErrors;
}): React.JSX.Element {
  const multiStatus = cloudDriftSurfaceStatus(
    multiEnabled,
    state.multi !== null,
    surfaceErrors.multi,
  );
  const awsStatus = cloudDriftSurfaceStatus(awsEnabled, state.aws !== null, surfaceErrors.aws);
  const unmanagedStatus = cloudDriftSurfaceStatus(
    awsEnabled,
    state.unmanaged !== null,
    surfaceErrors.unmanaged,
  );
  const importPlanStatus = cloudDriftSurfaceStatus(
    awsEnabled,
    state.importPlan !== null,
    surfaceErrors.importPlan,
  );

  return (
    <div
      aria-label="Cloud drift surface status"
      className="grid g-4 cloud-drift-summary"
      data-aws-status={awsStatus}
      data-import-plan-status={importPlanStatus}
      data-multi-status={multiStatus}
      data-unmanaged-status={unmanagedStatus}
    >
      <StatTile
        label="Multi-cloud drift"
        value={cloudDriftSurfaceValue(multiStatus, state.multi?.totalFindingsCount ?? 0)}
        color="var(--blue)"
        sub={
          multiStatus === "loaded"
            ? pageSub(state.multi?.truncated)
            : cloudDriftSurfaceLabel(multiStatus)
        }
      />
      <StatTile
        label="AWS drift"
        value={cloudDriftSurfaceValue(awsStatus, state.aws?.totalFindingsCount ?? 0)}
        color="var(--ember)"
        sub={awsStatus === "loaded" ? "bounded AWS findings" : cloudDriftSurfaceLabel(awsStatus)}
      />
      <StatTile
        label="Unmanaged"
        value={cloudDriftSurfaceValue(unmanagedStatus, state.unmanaged?.totalFindingsCount ?? 0)}
        color="var(--teal)"
        sub={
          unmanagedStatus === "loaded"
            ? "IaC management readback"
            : cloudDriftSurfaceLabel(unmanagedStatus)
        }
      />
      <StatTile
        label="Import candidates"
        value={cloudDriftSurfaceValue(importPlanStatus, state.importPlan?.readyCount ?? 0)}
        color="var(--violet)"
        sub={
          importPlanStatus === "loaded"
            ? `${state.importPlan?.refusedCount ?? 0} refused`
            : cloudDriftSurfaceLabel(importPlanStatus)
        }
      />
    </div>
  );
}
