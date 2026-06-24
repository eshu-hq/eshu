// ReasoningTrace.tsx — the live "steps" timeline of the agent loop.
import { Check, Sparkles, TriangleAlert } from "lucide-react";

import type { AskTraceStep } from "../../api/askEshu";
import { Panel } from "../atoms";
import { cx } from "./cx";
import { truthClassMeta } from "./truthClass";

/** A calm timeline of tool calls with supported/partial indicators. */
export function ReasoningTrace({
  steps,
  streaming
}: {
  readonly steps: readonly AskTraceStep[];
  readonly streaming: boolean;
}): React.JSX.Element | null {
  if (steps.length === 0 && !streaming) {
    return null;
  }
  const stepLabel = `${steps.length} ${steps.length === 1 ? "step" : "steps"}${streaming ? " · running…" : ""}`;
  return (
    <Panel
      action={
        streaming ? (
          <span aria-hidden className="ask-running">
            <i />
            <i />
            <i />
          </span>
        ) : (
          <span className="badge badge-teal">
            <Check aria-hidden size={12} /> done
          </span>
        )
      }
      className="ask-trace-panel"
      glyph={<Sparkles aria-hidden />}
      sub={stepLabel}
      title="Reasoning"
    >
      <ol aria-label="Reasoning steps" aria-live="polite" className="trace-timeline" role="log">
        {steps.map((step, index) => (
          <TraceStepRow key={index} step={step} />
        ))}
        {streaming ? (
          <li className="trace-step is-pending">
            <span className="trace-mark pulse">
              <i />
            </span>
            <div className="trace-main">
              <div className="trace-tool t-mut">Gathering evidence…</div>
            </div>
          </li>
        ) : null}
      </ol>
    </Panel>
  );
}

function TraceStepRow({ step }: { readonly step: AskTraceStep }): React.JSX.Element {
  const meta = truthClassMeta(step.truth_class);
  const ok = step.supported !== false;
  const markColor = ok ? meta.color : "var(--crit)";
  return (
    <li className={cx("trace-step", !ok && "is-warn")}>
      <span className="trace-mark" style={{ "--tm": markColor } as React.CSSProperties}>
        {ok ? <Check aria-hidden size={12} /> : <TriangleAlert aria-hidden size={12} />}
      </span>
      <div className="trace-main">
        <div className="trace-tool">
          <span className="mono">{step.tool}</span>
          {step.args ? <span className="trace-args">({argSummary(step.args)})</span> : null}
        </div>
        {!ok && step.err ? <div className="trace-err">{step.err}</div> : null}
      </div>
      <span
        className="trace-tc"
        style={{ "--tb": meta.color } as React.CSSProperties}
        title={`${meta.label} — ${meta.description}`}
      >
        <i />
        {ok ? meta.label : "unsupported"}
      </span>
    </li>
  );
}

function argSummary(args: Record<string, unknown>): string {
  return Object.keys(args)
    .map((key) => {
      const value = args[key];
      if (value === null || value === undefined || value === "") {
        return "";
      }
      return typeof value === "object" ? JSON.stringify(value) : StringPrimitive(value);
    })
    .filter((part) => part.length > 0)
    .join(" · ");
}

// StringPrimitive narrows an `unknown` to a string for primitive values
// (string/number/boolean/bigint/symbol). Objects and arrays must be
// JSON.stringify'd upstream — falling through to String() would produce
// "[object Object]" / "1,2,3" respectively.
function StringPrimitive(value: unknown): string {
  switch (typeof value) {
    case "string":
    case "number":
    case "boolean":
    case "bigint":
      return String(value);
    case "symbol":
      return value.description ?? "";
    default:
      return "";
  }
}
