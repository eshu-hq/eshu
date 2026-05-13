import type { ConsoleEnvironment } from "../config/environment";
import type { FreshnessState, RuntimeProfile } from "../api/envelope";

export interface RuntimeStatusSummary {
  readonly freshnessState: FreshnessState;
  readonly health: "demo" | "ready" | "degraded" | "unavailable";
  readonly profile: RuntimeProfile;
}

interface StatusStripProps {
  readonly environment: ConsoleEnvironment;
  readonly runtime: RuntimeStatusSummary;
}

export function StatusStrip({
  environment,
  runtime
}: StatusStripProps): React.JSX.Element {
  const environmentLabel =
    environment.mode === "demo" ? "Demo fixtures" : environment.apiBaseUrl;

  return (
    <aside className="status-strip" aria-label="Connected Eshu environment">
      <span>{environmentLabel}</span>
      <span>{runtime.health}</span>
      <span>{runtime.profile}</span>
      <span>{runtime.freshnessState}</span>
    </aside>
  );
}
