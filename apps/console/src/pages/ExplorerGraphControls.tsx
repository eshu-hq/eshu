import type { DeploymentGraphDetail } from "../api/eshuGraph";
import { LAYER_COLOR } from "../console/types";
import type { GraphLayer } from "../console/types";

export const EXPLORER_LAYERS: readonly GraphLayer[] = [
  "code",
  "deploy",
  "infra",
  "runtime",
  "security",
  "ops",
];

export function DeploymentDetailButton({
  busy,
  detail,
  onToggle,
  visible,
}: {
  readonly busy: boolean;
  readonly detail: DeploymentGraphDetail;
  readonly onToggle: () => void;
  readonly visible: boolean;
}): React.JSX.Element | null {
  if (!visible) return null;
  return (
    <button className="btn-ghost" disabled={busy} onClick={onToggle} type="button">
      {detail === "summary" ? "Show more deployment evidence" : "Show less deployment evidence"}
    </button>
  );
}

export function ExplorerLayerFilters({
  enabled,
  onToggle,
}: {
  readonly enabled: Readonly<Record<GraphLayer, boolean>>;
  readonly onToggle: (layer: GraphLayer) => void;
}): React.JSX.Element {
  return (
    <div className="explorer-filters">
      {EXPLORER_LAYERS.map((layer) => (
        <button
          key={layer}
          className={`layer-toggle ${enabled[layer] ? "on" : "off"}`}
          style={{ "--lc": LAYER_COLOR[layer] } as React.CSSProperties}
          onClick={() => onToggle(layer)}
        >
          <i style={{ background: LAYER_COLOR[layer] }} />
          <span style={{ textTransform: "capitalize" }}>{layer}</span>
        </button>
      ))}
    </div>
  );
}
