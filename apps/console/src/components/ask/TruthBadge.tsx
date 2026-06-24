// TruthBadge.tsx — the confidence chip that leads every answer.
import type { CSSProperties } from "react";

import { cx } from "./cx";
import { truthClassMeta } from "./truthClass";

/** A truth-class chip with label, confidence text and a hover/focus tooltip. */
export function TruthBadge({
  level,
  big
}: {
  readonly level: string;
  readonly big?: boolean;
}): React.JSX.Element {
  const meta = truthClassMeta(level);
  return (
    <span
      aria-label={`Confidence: ${meta.label}. ${meta.description}`}
      className={cx("truth-badge", big && "big")}
      role="note"
      style={{ "--tb": meta.color } as CSSProperties}
      tabIndex={0}
    >
      <i className="truth-badge-dot" />
      <span className="truth-badge-label">{meta.label}</span>
      <span className="truth-badge-conf">{meta.confidence}</span>
      <span className="truth-badge-tip" role="tooltip">
        {meta.description}
      </span>
    </span>
  );
}
