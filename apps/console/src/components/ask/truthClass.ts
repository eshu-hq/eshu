// truthClass.ts — presentation metadata for the Ask Eshu truth-class taxonomy.
//
// Every answer and every reasoning step leads with its truth class so the user
// reads confidence before prose: wrong-looking-confident is worse than
// honest-and-partial. Colors reference the shared console design tokens.
import type { AskTruthClass } from "../../api/askEshu";

/** Display metadata for one truth class. */
export interface TruthClassMeta {
  readonly label: string;
  readonly color: string;
  readonly confidence: string;
  readonly description: string;
}

const FALLBACK: TruthClassMeta = {
  label: "Fallback",
  color: "var(--ember)",
  confidence: "Lower confidence",
  description: "Best-effort answer assembled without a primary evidence source."
};

const TRUTH_CLASS: Record<AskTruthClass, TruthClassMeta> = {
  deterministic: {
    label: "Deterministic",
    color: "var(--teal)",
    confidence: "High confidence",
    description: "Read directly from the graph — exact facts from indexed source, manifests or provider APIs."
  },
  derived: {
    label: "Derived",
    color: "var(--med)",
    confidence: "Medium confidence",
    description: "Inferred from manifests, version ranges or relationship roll-ups — not observed directly."
  },
  semantic_observation: {
    label: "Observed",
    color: "var(--blue)",
    confidence: "Medium confidence",
    description: "From runtime telemetry — traces, metrics or logs Eshu observed live."
  },
  code_hint: {
    label: "Code hint",
    color: "var(--violet)",
    confidence: "Lower confidence",
    description: "A heuristic from static code analysis; may include false positives."
  },
  fallback: FALLBACK,
  unsupported: {
    label: "Evidence only",
    color: "var(--subtle)",
    confidence: "No narration",
    description: "No narrated answer was produced — only the evidence Eshu gathered is shown."
  }
};

/** Resolve display metadata for a truth class, defaulting to `fallback`. */
export function truthClassMeta(value: string | undefined): TruthClassMeta {
  if (value && value in TRUTH_CLASS) {
    return TRUTH_CLASS[value as AskTruthClass];
  }
  return FALLBACK;
}
