import { GitBranch } from "lucide-react";

import { siteContent } from "./siteContent";
import type { CommandDemo } from "./siteContent";

/** Renders the static source-to-runtime graph beside the hero copy. */
export function SourceRuntimeGraph({
  selectedCommand
}: {
  readonly selectedCommand: CommandDemo;
}): React.JSX.Element {
  return (
    <aside className="product-visual" aria-label="Source to runtime graph">
      <div className="visual-header">
        <span>{selectedCommand.summary}</span>
        <GitBranch aria-hidden="true" />
      </div>
      <div className="visual-canvas">
        <img className="truth-mark" src="/brand/eshu-icon.svg" alt="" aria-hidden="true" />
        <svg aria-hidden="true" viewBox="0 0 760 360" className="path-svg">
          <path d="M82 182 C160 96 250 92 340 172 S540 276 668 106" />
          <path d="M340 172 C424 102 500 96 574 146" />
          <path d="M340 172 C430 226 508 244 638 246" />
        </svg>
        {siteContent.demoTrace.nodes.map((node) => (
          <span
            className={
              node.id === selectedCommand.activeNodeId
                ? `graph-node node-${node.id} is-active`
                : `graph-node node-${node.id}`
            }
            key={node.id}
          >
            {node.label}
            <small>{node.detail}</small>
          </span>
        ))}
        <span className="truth-label">evidence-backed</span>
      </div>
    </aside>
  );
}
