export interface StepResult {
  readonly id: string;
  readonly status: "pass" | "fail" | "blocked";
  readonly detail: string;
  readonly ms: number;
}

export type AuthE2EStep = (id: string, fn: () => Promise<string>) => Promise<void>;

export async function recordAuthE2EStep(
  results: StepResult[],
  id: string,
  fn: () => Promise<string>,
): Promise<void> {
  const start = Date.now();
  try {
    const detail = await fn();
    results.push({ id, status: "pass", detail, ms: Date.now() - start });
    process.stdout.write(`  PASS ${id} (${Date.now() - start}ms): ${detail}\n`);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    results.push({ id, status: "fail", detail, ms: Date.now() - start });
    process.stdout.write(`  FAIL ${id} (${Date.now() - start}ms): ${detail}\n`);
  }
}
