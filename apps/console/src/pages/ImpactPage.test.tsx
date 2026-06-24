import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { ImpactPage } from "./ImpactPage";
import type { EshuApiClient } from "../api/client";
import { modelFromSnapshot, emptySnapshot } from "../console/liveModel";

describe("ImpactPage", () => {
  it("loads a deep-linked service impact review with graph, truth, and trace evidence", async () => {
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/impact/change-surface/investigate") {
          return {
            data: changeSurfacePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.change_surface")
          };
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return {
            data: deploymentTracePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.deployment_chain")
          };
        }
        throw new Error(`unexpected path ${path}`);
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/impact?kind=service&target=catalog-api&repoId=repository%3Ar_catalog"]}>
        <ImpactPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Impact" })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText("3 nodes · 2 edges")).toBeInTheDocument();
    });
    expect(screen.getAllByText("sample-communicator").length).toBeGreaterThan(0);
    expect(screen.getAllByText("terraform-stack-node10").length).toBeGreaterThan(0);
    expect(screen.getByText("platform_impact.change_surface")).toBeInTheDocument();
    expect(screen.getAllByText("derived").length).toBeGreaterThan(0);
    expect(screen.getAllByText("fresh").length).toBeGreaterThan(0);
    expect(screen.getByText("catalog-api reaches runtime through a deployment-config repository.")).toBeInTheDocument();
    expect(screen.getByText("Blast radius requires a repository, Terraform module, Crossplane XRD, or SQL table anchor.")).toBeInTheDocument();
  });

  it("submits the form into a deep-linkable review URL", async () => {
    const calls: string[] = [];
    const client = {
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/blast-radius") {
          return {
            data: {
              affected: [{ hops: 1, repo: "consumer-api", repo_id: "repository:r_consumer" }],
              affected_count: 1,
              limit: 25,
              target: "catalog-api",
              target_type: "repository",
              truncated: false
            },
            error: null,
            truth: truthEnvelope("platform_impact.blast_radius")
          };
        }
        if (path === "/api/v0/impact/change-surface/investigate") {
          return {
            data: changeSurfacePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.change_surface")
          };
        }
        throw new Error(`unexpected path ${path}`);
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/impact"]}>
        <ImpactPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Entity type"), { target: { value: "repository" } });
    fireEvent.change(screen.getByLabelText("Entity target"), { target: { value: "catalog-api" } });
    fireEvent.click(screen.getByRole("button", { name: "Review impact" }));

    await waitFor(() => {
      expect(calls).toEqual([
        "/api/v0/impact/blast-radius",
        "/api/v0/impact/change-surface/investigate"
      ]);
    });
    expect(screen.getAllByText("consumer-api").length).toBeGreaterThan(0);
  });

  it("auto-loads an impact review for a default catalog service on open", async () => {
    const calls: string[] = [];
    const client = {
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/change-surface/investigate") {
          return { data: changeSurfacePayload(), error: null, truth: truthEnvelope("platform_impact.change_surface") };
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return { data: deploymentTracePayload(), error: null, truth: truthEnvelope("platform_impact.deployment_chain") };
        }
        throw new Error(`unexpected path ${path}`);
      }
    } as unknown as EshuApiClient;

    const model = modelFromSnapshot({
      ...emptySnapshot("live"),
      services: [{ id: "svc:acme-app", name: "acme-app", kind: "service", repo: "acme-app", environments: [], truth: "exact", freshness: "fresh" }]
    });

    render(
      <MemoryRouter initialEntries={["/impact"]}>
        <ImpactPage client={client} model={model} />
      </MemoryRouter>
    );

    await waitFor(() => expect(calls).toContain("/api/v0/impact/change-surface/investigate"));
    expect(screen.getByLabelText<HTMLInputElement>("Entity target").value).toBe("acme-app");
  });

  it("loads a deployable-unit investigation packet from explicit scope inputs", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.startsWith("/api/v0/investigations/deployable-unit/packet")) {
          return {
            data: {
              answer: { summary: "Deployable-unit packet is supported.", supported: true, truth_class: "exact" },
              bounds: { max_source_facts: 200, truncated: false },
              graph_answers: [{ id: "graph:deploy:1", present: true, relation: "DEPLOYS" }],
              identity: { family: "deployable_unit", scope: { scope_id: "scope-1", generation_id: "generation-1" } },
              missing_evidence: [{ hop: "runtime_workload", reason: "workload edge missing" }],
              packet_id: "investigation-evidence-packet:deploy-demo",
              reducer_decisions: [{ id: "decision:deploy:1", state: "admitted" }],
              redaction: { profile: "share_safe_v2" },
              reproduce: [{ kind: "http", route: "/api/v0/investigations/deployable-unit/packet" }],
              schema: "investigation_evidence_packet.v2",
              source_facts: [{ evidence_family: "admission_decision", fact_id: "fact:deploy:1" }],
              validation: { valid: true }
            },
            error: null,
            truth: truthEnvelope("deployable_unit.packet")
          };
        }
        throw new Error(`unexpected path ${path}`);
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/impact?scope_id=scope-1&generation_id=generation-1&repo_id=repo%3A%2F%2Fteam%2Fapi"]}>
        <ImpactPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(screen.getByLabelText<HTMLInputElement>("Packet repository ID").value).toBe("repo://team/api");
    fireEvent.click(screen.getByRole("button", { name: "Load deployable-unit packet" }));

    await waitFor(() => {
      expect(screen.getByText("investigation-evidence-packet:deploy-demo")).toBeInTheDocument();
    });
    expect(screen.getByText("runtime_workload")).toBeInTheDocument();
    expect(calls).toContain(
      "/api/v0/investigations/deployable-unit/packet?scope_id=scope-1&generation_id=generation-1&repository_id=repo%3A%2F%2Fteam%2Fapi&max_source_facts=50"
    );
  });
});

function truthEnvelope(capability: string) {
  return {
    basis: "hybrid_graph_and_content",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative"
  };
}

function changeSurfacePayload(): Record<string, unknown> {
  return {
    code_surface: {
      changed_files: [{ relative_path: "server/routes/leads.ts", repo_id: "repository:r_catalog" }],
      matched_file_count: 1,
      source_backends: ["postgres_content_store"],
      symbol_count: 1,
      touched_symbols: [{
        entity_id: "entity:post-lead",
        entity_name: "postLead",
        entity_type: "Function",
        language: "typescript",
        relative_path: "server/routes/leads.ts"
      }]
    },
    coverage: {
      direct_count: 1,
      limit: 25,
      max_depth: 4,
      query_shape: "resolved_change_surface_traversal",
      transitive_count: 1,
      truncated: false
    },
    direct_impact: [{
      depth: 1,
      id: "workload:sample-communicator",
      labels: ["Workload"],
      name: "sample-communicator",
      repo_id: "repository:r_sample"
    }],
    impact_summary: {
      direct_count: 1,
      total_count: 2,
      transitive_count: 1
    },
    source_backend: "hybrid_graph_and_content",
    target_resolution: {
      input: "catalog-api",
      selected: {
        id: "workload:catalog-api",
        labels: ["Workload"],
        name: "catalog-api"
      },
      status: "resolved",
      target_type: "service",
      truncated: false
    },
    transitive_impact: [{
      depth: 2,
      id: "repo:terraform-stack-node10",
      labels: ["Repository"],
      name: "terraform-stack-node10",
      repo_id: "repository:r_stack"
    }],
    truncated: false
  };
}

function deploymentTracePayload(): Record<string, unknown> {
  return {
    cloud_resources: [{ id: "cloud:queue", name: "lead-events", resource_type: "aws_sqs_queue" }],
    deployment_overview: {
      cloud_resource_count: 1,
      deployment_source_count: 1,
      environments: ["prod"],
      k8s_resource_count: 1
    },
    deployment_sources: [{
      path: "applications/catalog-api.yaml",
      repo_name: "deployment-config",
      relationship_type: "DEPLOYS_FROM"
    }],
    k8s_resources: [{ entity_name: "catalog-api", kind: "Deployment" }],
    service_name: "catalog-api",
    story: "catalog-api reaches runtime through a deployment-config repository.",
    workload_id: "workload:catalog-api"
  };
}
