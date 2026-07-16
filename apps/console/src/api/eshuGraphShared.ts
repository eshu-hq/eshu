import type { GraphLayer } from "../console/types";

const VERB_LAYER: Record<string, GraphLayer> = {
  CALLS: "code",
  IMPORTS: "code",
  INHERITS: "code",
  OVERRIDES: "code",
  REFERENCES: "code",
  DEPLOYS_FROM: "deploy",
  DEPLOYS_HELM: "deploy",
  PACKAGES: "deploy",
  BUILDS: "deploy",
  DISCOVERS_CONFIG_IN: "deploy",
  DECLARED_BY: "infra",
  STORES_IN: "infra",
  ASSUMES_ROLE: "infra",
  RUNS_IN: "runtime",
  RUNS_AS: "runtime",
  DEPENDS_ON: "runtime",
  EXPOSES: "runtime",
  AFFECTED_BY: "security",
  OBSERVED_INCIDENT: "ops",
  TRACKED_BY: "ops",
};

export function layerFor(verb: string): GraphLayer {
  return VERB_LAYER[verb.toUpperCase()] ?? "runtime";
}

export function kindFor(type: string | undefined): string {
  const normalized = (type ?? "").toLowerCase();
  if (normalized.includes("service")) return "service";
  if (normalized.includes("workload") || normalized.includes("deployment")) return "workload";
  if (normalized.includes("repo")) return "repo";
  if (
    normalized.includes("module") ||
    normalized.includes("package") ||
    normalized.includes("library")
  )
    return "library";
  if (
    normalized.includes("function") ||
    normalized.includes("class") ||
    normalized.includes("symbol")
  )
    return "client";
  if (normalized.includes("resource") || normalized.includes("aws")) return "aws";
  return "service";
}

export function cleanText(value: string | undefined): string {
  return value?.trim() ?? "";
}
