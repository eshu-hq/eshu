import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "../api/client";
import { ExposurePathPage } from "./ExposurePathPage";

// ExposurePathPage renders a code-to-cloud exposure finding. It must:
// - render a reachable-sink finding with the node chain, sink, severity, the
//   derived label, and the exposure rank
// - render an unresolved finding with the coverage reason and WITHOUT implying a
//   path exists
// - never fabricate a path
describe("ExposurePathPage", () => {
  it("renders a reachable-sink finding with chain, severity, rank, and derived label", async () => {
    const client = {
      post: async (path: string) => {
        expect(path).toBe("/api/v0/impact/trace-exposure-path");
        return {
          data: reachableWire(),
          error: null,
          truth: {
            capability: "platform_impact.exposure_path",
            level: "derived",
            profile: "production",
            freshness: { state: "fresh" }
          }
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?source=createWidgetHandler&repoId=repository%3Ar_example"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));

    expect(await screen.findByText("Reaches IAM privileged action")).toBeInTheDocument();
    // Conservative truth-state badge text (not just color).
    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    // Exposure rank surfaced with text.
    expect(screen.getByText("internet exposed")).toBeInTheDocument();
    // Severity surfaced with text.
    expect(screen.getByText("critical")).toBeInTheDocument();
    // Derived label.
    expect(screen.getAllByText("derived").length).toBeGreaterThan(0);
    // The node chain: internet -> handler -> ... -> sink.
    expect(screen.getByText("internet")).toBeInTheDocument();
    // The source name appears in the finding summary and in the chain node.
    expect(screen.getAllByText("createWidgetHandler").length).toBeGreaterThan(0);
    expect(screen.getByText("persistWidget")).toBeInTheDocument();
    expect(screen.getByText("admin-policy")).toBeInTheDocument();
    // Honest reason.
    expect(screen.getByText(/internet-exposed handler/)).toBeInTheDocument();
  });

  it("renders an unresolved finding with the coverage reason and no fabricated path", async () => {
    const client = {
      post: async () => ({
        data: unresolvedWire(),
        error: null,
        truth: {
          capability: "platform_impact.exposure_path",
          level: "derived",
          profile: "production",
          freshness: { state: "fresh" }
        }
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?source=drainQueueHandler"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));

    expect(await screen.findByText("No proven exposure path")).toBeInTheDocument();
    expect(
      screen.getByText("code-to-cloud bridge edge is not materialized; cloud-sink segment is unresolved")
    ).toBeInTheDocument();
    expect(screen.getAllByText("unresolved").length).toBeGreaterThan(0);
    // No path was fabricated: the "internet" chain entry and a sink card title
    // must NOT be present.
    expect(screen.queryByText("internet")).not.toBeInTheDocument();
    expect(screen.queryByText(/^Reaches /)).not.toBeInTheDocument();
  });

  it("does not render an internet origin for a resolved network_reachable path", async () => {
    const client = {
      post: async () => ({
        data: resolvedWireWithRank("network_reachable", "high"),
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?source=drainQueueHandler"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));

    // The path resolves, so a chain renders...
    expect(await screen.findByText("Reaches IAM privileged action")).toBeInTheDocument();
    // ...but it must NOT claim internet reachability the backend did not prove.
    expect(screen.queryByText("internet")).not.toBeInTheDocument();
    // It surfaces the proven origin instead.
    expect(screen.getByText("network boundary")).toBeInTheDocument();
  });

  it("renders no synthetic origin for a resolved internal path", async () => {
    const client = {
      post: async () => ({
        data: resolvedWireWithRank("internal", "medium"),
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?source=drainQueueHandler"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));

    expect(await screen.findByText("Reaches IAM privileged action")).toBeInTheDocument();
    // An internal source proves no external reachability: no internet and no
    // network-boundary origin is drawn.
    expect(screen.queryByText("internet")).not.toBeInTheDocument();
    expect(screen.queryByText("network boundary")).not.toBeInTheDocument();
  });

  it("shows an explicit error when the trace request fails, with no path", async () => {
    const client = {
      post: async () => {
        throw new Error("HTTP 503");
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?source=createWidgetHandler"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));

    await waitFor(() => expect(screen.getByText(/503/)).toBeInTheDocument());
    expect(screen.queryByText("internet")).not.toBeInTheDocument();
  });

  it("requires a source before tracing", () => {
    const client = { post: async () => ({ data: null, error: null, truth: null }) } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );
    fireEvent.click(screen.getByRole("button", { name: "Trace exposure" }));
    expect(screen.getByText("A source handler name or entity id is required.")).toBeInTheDocument();
  });
});

function reachableWire(): Record<string, unknown> {
  return {
    source: {
      entity_id: "entity:e_handler",
      name: "createWidgetHandler",
      labels: ["Function", "HttpHandler"]
    },
    source_kind: "http_handler",
    exposure_rank: "internet_exposed",
    truth_label: "derived",
    state: "exact",
    paths: [
      {
        nodes: [
          { entity_id: "entity:e_handler", name: "createWidgetHandler", labels: ["Function", "HttpHandler"] },
          { entity_id: "entity:e_persist", name: "persistWidget", labels: ["Function"] }
        ],
        sink: {
          kind: "iam_privileged_action",
          display_name: "IAM privileged action",
          node: { entity_id: "cloud:c_iam", name: "admin-policy", labels: ["IamPolicy"] }
        },
        depth: 2,
        state: "exact",
        severity: "critical",
        reason:
          "internet-exposed handler transitively reaches a privileged IAM action; no authentication gate is modeled in Level 1, so this is a derived upper-bound severity"
      }
    ],
    coverage: { max_depth: 5, paths_found: 1, truncated: false, unresolved_reason: "" }
  };
}

// resolvedWireWithRank builds a finding that resolves a path (so a chain renders)
// under a chosen exposure rank, used to prove the leading chain node is not a
// hard-coded "internet" entry for non-internet ranks.
function resolvedWireWithRank(
  rank: "network_reachable" | "internal",
  severity: "high" | "medium"
): Record<string, unknown> {
  return {
    source: {
      entity_id: "entity:e_drain",
      name: "drainQueueHandler",
      labels: ["Function", "MessageConsumer"]
    },
    source_kind: "message_consumer",
    exposure_rank: rank,
    truth_label: "derived",
    state: "exact",
    paths: [
      {
        nodes: [
          { entity_id: "entity:e_drain", name: "drainQueueHandler", labels: ["Function"] }
        ],
        sink: {
          kind: "iam_privileged_action",
          display_name: "IAM privileged action",
          node: { entity_id: "cloud:c_iam", name: "admin-policy", labels: ["IamPolicy"] }
        },
        depth: 1,
        state: "exact",
        severity,
        reason: `${rank.replace(/_/g, " ")} handler reaches an IAM privileged action sink`
      }
    ],
    coverage: { max_depth: 5, paths_found: 1, truncated: false, unresolved_reason: "" }
  };
}

function unresolvedWire(): Record<string, unknown> {
  return {
    source: {
      entity_id: "entity:e_drain",
      name: "drainQueueHandler",
      labels: ["Function", "MessageConsumer"]
    },
    source_kind: "message_consumer",
    exposure_rank: "network_reachable",
    truth_label: "derived",
    state: "unresolved",
    paths: [],
    coverage: {
      max_depth: 5,
      paths_found: 0,
      truncated: false,
      unresolved_reason:
        "code-to-cloud bridge edge is not materialized; cloud-sink segment is unresolved"
    }
  };
}
