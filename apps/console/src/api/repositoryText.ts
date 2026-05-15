export function deploymentEvidenceSummary(
  family: string,
  sourceRepo: string,
  count: number,
  path: string
): string {
  if (family === "argocd") {
    return `${sourceRepo} has ${count} ArgoCD ApplicationSet evidence item(s), including ${path}.`;
  }
  return `${sourceRepo} has ${count} Helm chart or values evidence item(s), including ${path}.`;
}

export function isPresent(value: string | undefined): value is string {
  return value !== undefined && value.trim().length > 0;
}

export function joinHuman(values: readonly string[]): string {
  if (values.length <= 2) {
    return values.join(" and ");
  }
  return `${values.slice(0, -1).join(", ")}, and ${values[values.length - 1]}`;
}

export function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
