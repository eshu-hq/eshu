import { screen } from "@testing-library/react";

import type { RepoListItem } from "../api/repoCatalog";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

export function repoItem(id: string, name: string): RepoListItem {
  return {
    groupKey: "",
    groupKind: "",
    groupReason: "",
    groupSource: "",
    groupTruth: "",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: name,
  };
}

export function liveModelWithRepositoriesOnly(): ConsoleModel {
  return modelFromSnapshot({
    ...emptySnapshot(),
    provenance: { runtime: "live" },
    runtime: {
      deadLetters: 0,
      inFlight: 0,
      indexStatus: "complete",
      instances: 0,
      platforms: 0,
      profile: "local_full_stack",
      queueOutstanding: 0,
      repositories: 951,
      succeeded: 951,
      workloads: 0,
    },
  });
}

export function liveModelWithServices(): ConsoleModel {
  return modelFromSnapshot({
    ...emptySnapshot(),
    services: [
      {
        environments: ["prod"],
        freshness: "fresh",
        id: "svc-checkout",
        kind: "service",
        name: "checkout-service",
        repo: "checkout",
        truth: "exact",
      },
    ],
    provenance: { services: "live" },
    runtime: {
      deadLetters: 0,
      inFlight: 0,
      indexStatus: "complete",
      instances: 0,
      platforms: 0,
      profile: "local_full_stack",
      queueOutstanding: 0,
      repositories: 1,
      succeeded: 1,
      workloads: 1,
    },
  });
}

export function liveModelWithTrivialThenHub(): ConsoleModel {
  return modelFromSnapshot({
    ...emptySnapshot(),
    services: [
      {
        environments: [],
        freshness: "fresh",
        id: "svc-trivial",
        kind: "service",
        name: "trivial-service",
        repo: "trivial",
        truth: "exact",
      },
      {
        environments: [],
        freshness: "fresh",
        id: "svc-hub",
        kind: "service",
        name: "hub-service",
        repo: "hub",
        truth: "exact",
      },
    ],
    provenance: { services: "live" },
    runtime: {
      deadLetters: 0,
      inFlight: 0,
      indexStatus: "complete",
      instances: 0,
      platforms: 0,
      profile: "local_full_stack",
      queueOutstanding: 0,
      repositories: 2,
      succeeded: 2,
      workloads: 2,
    },
  });
}

export function resolveEntityMap(
  resolvers: Map<string, (value: unknown) => void>,
  from: string,
  related: string,
): void {
  const resolve = resolvers.get(from);
  if (!resolve) throw new Error(`missing resolver for ${from}`);
  resolvers.delete(from);
  const name = from.includes(":") ? from.slice(from.indexOf(":") + 1) : from;
  resolve({
    data: {
      from,
      resolution: {
        candidates: [
          { id: from.includes(":") ? from : `workload:${from}`, labels: ["Workload"], name },
        ],
      },
      evidence: {
        relationships: [
          {
            direction: "outgoing",
            entity_id: `workload:${related}`,
            entity_labels: ["Workload"],
            entity_name: related,
            relationship_type: "DEPENDS_ON",
          },
        ],
      },
    },
    error: null,
    truth: null,
  });
}

export function graphLabel(label: string): HTMLElement {
  const text = screen
    .getAllByText(label)
    .find((element) => element.tagName.toLowerCase() === "text");
  if (!text) throw new Error(`missing graph label ${label}`);
  return text;
}

export function requestFrom(body: unknown): string {
  if (
    typeof body === "object" &&
    body !== null &&
    "from" in body &&
    typeof body.from === "string"
  ) {
    return body.from;
  }
  return "";
}

export function requestName(body: unknown): string {
  if (
    typeof body === "object" &&
    body !== null &&
    "name" in body &&
    typeof body.name === "string"
  ) {
    return body.name;
  }
  return "";
}
