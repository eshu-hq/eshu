import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { vi } from "vitest";

import { ServiceEvidenceGraphPage } from "./ServiceEvidenceGraphPage";
import type { EshuApiClient } from "../api/client";
import type { EshuEnvelope } from "../api/envelope";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

export function liveModel() {
  return modelFromSnapshot(emptySnapshot("live"));
}

export function modelWithService(name: string) {
  return modelFromSnapshot({
    ...emptySnapshot("live"),
    services: [
      {
        id: `svc:${name}`,
        name,
        kind: "service",
        repo: `${name}-repo`,
        environments: [],
        truth: "exact",
        freshness: "fresh",
      },
    ],
  });
}

function storyEnvelope(): EshuEnvelope<Record<string, unknown>> {
  return {
    data: {
      service_identity: { service_id: "svc-1", service_name: "payments", repo_id: "svc-repo" },
      upstream_dependencies: [
        {
          source: "billing",
          source_repo_id: "up-1",
          target_repo_id: "svc-repo",
          relationship_type: "DEPENDS_ON",
          confidence: 0.9,
        },
      ],
      downstream_consumers: {},
    },
    error: null,
    truth: {
      capability: "service.story.read",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "local_authoritative",
    },
  };
}

export function deriveEnvelope(
  packet: Record<string, unknown>,
): EshuEnvelope<Record<string, unknown>> {
  return {
    data: { visualization_packet: packet },
    error: null,
    truth: {
      capability: "visualization.derive",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "local_authoritative",
    },
  };
}

export function supportedPacket(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    view: "service_story",
    title: "payments",
    supported: true,
    nodes: [
      {
        id: "viznode:service",
        type: "service",
        label: "payments",
        category: "service",
        evidence_handle: { kind: "entity", repo_id: "svc-repo", entity_id: "svc-1" },
      },
      {
        id: "viznode:up-1",
        type: "repository",
        label: "billing",
        category: "upstream",
        truth_label: "exact",
        evidence_handle: {
          kind: "entity",
          repo_id: "up-1",
          entity_id: "up-1",
          evidence_family: "repository",
        },
      },
    ],
    edges: [
      {
        id: "vizedge:1",
        source: "viznode:up-1",
        target: "viznode:service",
        relationship: "DEPENDS_ON",
        truth_label: "exact",
      },
    ],
    truth: { level: "exact", basis: "authoritative_graph", freshness: { state: "fresh" } },
    limits: {
      max_nodes: 60,
      max_edges: 120,
      ordering: "stable_id",
      node_count: 2,
      edge_count: 1,
    },
    truncation: { truncated: false, dropped_node_count: 0, dropped_edge_count: 0 },
    limitations: [],
    recommended_next_calls: [],
    ...overrides,
  };
}

export function clientFor(deriveData: EshuEnvelope<Record<string, unknown>>): {
  client: EshuApiClient;
  paths: string[];
} {
  const paths: string[] = [];
  const client = {
    get: vi.fn(async (path: string) => {
      paths.push(path);
      return storyEnvelope();
    }),
    post: vi.fn(async (path: string) => {
      paths.push(path);
      return deriveData;
    }),
  } as unknown as EshuApiClient;
  return { client, paths };
}

export function renderServiceEvidenceGraphAt(path: string, client: EshuApiClient) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/service-story"
          element={
            <ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} />
          }
        />
        <Route
          path="/service-story/:serviceName"
          element={
            <ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} />
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}
