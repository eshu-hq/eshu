// api/demoRequestMatch.ts
// Small request-body/param matching helpers shared by the per-surface demo
// scope-guard predicates (demoCloudFixtures.ts, demoImpactFixtures.ts,
// demoCicdFixtures.ts). These guards only run once a request has already
// been routed to a lazily loaded fixture module (issue #5139), so this
// helper module is pulled in alongside them rather than sitting in the
// eagerly loaded main bundle.
export function objectBody(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

export function field(record: Record<string, unknown>, name: string): string {
  const value = record[name];
  return typeof value === "string" ? value.trim() : "";
}

export function optionalFieldMatches(
  record: Record<string, unknown>,
  name: string,
  expected: string,
): boolean {
  const value = field(record, name);
  return value.length === 0 || value === expected;
}

export function optionalParamMatches(
  params: URLSearchParams,
  name: string,
  expected: string,
): boolean {
  const value = params.get(name)?.trim() ?? "";
  return value.length === 0 || value === expected;
}
