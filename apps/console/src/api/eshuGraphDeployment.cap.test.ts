import { describe, expect, it } from "vitest";

import { deploymentStoryToGraph } from "./eshuGraph";

// The server caps entrypoints and network_paths at 50 rows and reports the
// pre-cut totals (#7169). The graph's "not shown" summaries must count from
// those totals, not from the capped arrays, or a 671-entrypoint service reads
// as having 49 rows hidden.
function cappedContext(total: number, shown: number) {
  const hosts = Array.from(
    { length: shown },
    (_, i) => `svc-${String(i).padStart(3, "0")}.qa.test`,
  );
  return {
    id: "workload:checkout-api",
    name: "checkout-api",
    entrypoints: hosts.map((target) => ({ type: "hostname", target, visibility: "public" })),
    network_paths: hosts.map((from) => ({
      path_type: "hostname_to_runtime",
      from_type: "hostname",
      from,
      to_type: "runtime_platform",
      to: "checkout-eks",
    })),
    result_limits: {
      limit: 50,
      entrypoint_count: total,
      network_path_count: total,
      truncated: total > shown,
    },
  };
}

function summaryLabel(
  graph: ReturnType<typeof deploymentStoryToGraph>,
  id: string,
): string | undefined {
  return graph.nodes.find((node) => node.id === `summary:${id}`)?.label;
}

describe("deployment graph omission counts under the server cap (#7169)", () => {
  it("counts hidden entrypoints and network paths from the context totals", () => {
    const graph = deploymentStoryToGraph(
      cappedContext(671, 50),
      "checkout-api",
      {},
      { detail: "summary" },
    );

    expect(summaryLabel(graph, "entrypoints")).toBe("670 entrypoints not shown");
    expect(summaryLabel(graph, "network_paths")).toBe("670 network paths not shown");
  });

  it("counts hidden entrypoints and network paths from the trace limits", () => {
    const context = cappedContext(671, 50);
    const trace = {
      entrypoints: context.entrypoints,
      network_paths: context.network_paths,
      entrypoint_limits: { limit: 50, total: 671, truncated: true },
      network_path_limits: { limit: 50, total: 671, truncated: true },
    };
    const graph = deploymentStoryToGraph(
      { id: context.id, name: context.name },
      "checkout-api",
      trace,
      { detail: "summary" },
    );

    expect(summaryLabel(graph, "entrypoints")).toBe("670 entrypoints not shown");
    expect(summaryLabel(graph, "network_paths")).toBe("670 network paths not shown");
  });

  it("falls back to the returned rows when the server reports no totals", () => {
    const context = cappedContext(4, 4);
    const { result_limits: _unused, ...withoutLimits } = context;
    void _unused;
    const graph = deploymentStoryToGraph(withoutLimits, "checkout-api", {}, { detail: "summary" });

    expect(summaryLabel(graph, "entrypoints")).toBe("3 entrypoints not shown");
    expect(summaryLabel(graph, "network_paths")).toBe("3 network paths not shown");
  });
});
