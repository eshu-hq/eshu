// Type declarations for console-bundle-budget.mjs so its pure evaluator can be
// imported from strict TypeScript (apps/console/src/consoleBundleBudget.test.ts)
// without an implicit-any error. Keep these signatures in lockstep with the
// exports in console-bundle-budget.mjs.

/** Stable budget key produced by {@link classifyAsset} for a built JS chunk. */
export type BudgetKey =
  | "main"
  | "react-vendor"
  | "d3"
  | "icons"
  | "mermaid"
  | "cytoscape"
  | "wardley"
  | "katex"
  | "async-chunk";

/** A built asset file with its raw (un-gzipped) minified size in bytes. */
export interface AssetFile {
  readonly name: string;
  readonly bytes: number;
}

/** A classified chunk paired with the budget it was checked against. */
export interface CheckedChunk {
  readonly key: BudgetKey;
  readonly name: string;
  readonly bytes: number;
  readonly budgetBytes: number;
}

/** Inputs to the pure budget evaluator. */
export interface EvaluateBundleBudgetInput {
  readonly files: readonly AssetFile[];
  readonly budgets: Readonly<Record<string, number>>;
  readonly defaultBudgetBytes: number;
  /** Budget key that MUST be present; a missing anchor is a hard failure. */
  readonly requireAnchor?: BudgetKey | null;
}

/** Result of evaluating the bundle budget over a set of built assets. */
export interface EvaluateBundleBudgetResult {
  readonly ok: boolean;
  readonly checked: readonly CheckedChunk[];
  readonly violations: readonly CheckedChunk[];
  readonly missingAnchor: BudgetKey | null;
}

/** Maximum allowed raw minified size in bytes, keyed by budget key. */
export const BUDGETS: Readonly<Record<string, number>>;

/** Fallback budget in bytes for an async chunk without an explicit budget. */
export const DEFAULT_ASYNC_BUDGET_BYTES: number;

/** Map a built asset filename to its budget key, or null when not a budgeted JS chunk. */
export function classifyAsset(name: string): BudgetKey | null;

/** Pure decision core: classify each file and report budget violations. */
export function evaluateBundleBudget(
  input: EvaluateBundleBudgetInput
): EvaluateBundleBudgetResult;
