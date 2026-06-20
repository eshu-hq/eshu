import { render, screen } from "@testing-library/react";
import type { EshuApiClient } from "../api/client";
import { FreshnessCausalityPage } from "./FreshnessCausalityPage";

// FreshnessCausalityPage renders the freshness causality read model and must
// show fresh, building, stale, and tenant-scoped (permission-hidden) states
// without fabricating data.
function clientReturning(data: unknown): EshuApiClient {
  return {
    get: async () => ({
      data,
      error: null,
      truth: {
        profile: "production",
        level: "exact",
        capability: "freshness.causality",
        freshness: { state: "fresh" },
      },
    }),
  } as unknown as EshuApiClient;
}

const baseCauses = [
  { cause: "pending_repo_generation", observed: false, observability: "runtime", detail: "d", next_check: { reason: "check generations" } },
  { cause: "reducer_backlog", observed: false, observability: "runtime", detail: "d", next_check: { reason: "check queue" } },
  { cause: "dead_lettered_domain", observed: false, observability: "runtime", detail: "d", next_check: { reason: "check dead letters" } },
  { cause: "missing_collector_completion", observed: false, observability: "runtime", detail: "d", next_check: { reason: "check collectors" } },
  { cause: "content_coverage_unavailable", observed: false, observability: "per_answer", detail: "d", next_check: { reason: "per answer" } },
  { cause: "unsupported_profile", observed: false, observability: "per_answer", detail: "d", next_check: { reason: "per answer" } },
  { cause: "retention_expired", observed: false, observability: "per_answer", detail: "d", next_check: { reason: "per answer" } },
];

describe("FreshnessCausalityPage", () => {
  it("shows the loading state until the model resolves", () => {
    const client = { get: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<FreshnessCausalityPage client={client} />);
    expect(screen.getByText("Loading freshness causality…")).toBeInTheDocument();
  });

  it("renders the fresh state with no observed causes", async () => {
    const client = clientReturning({
      state: "fresh",
      scoped: false,
      causes: baseCauses,
      generations: { active: 4, pending: 0, completed: 9, superseded: 0, failed: 0 },
      pending_projection: { outstanding: 0, dead_letter: 0, domains: 0 },
      recent_transitions: [],
    });
    render(<FreshnessCausalityPage client={client} />);
    expect(await screen.findByText("No freshness causes are currently observed in the runtime.")).toBeInTheDocument();
    expect(screen.getByText(/state: fresh/)).toBeInTheDocument();
  });

  it("renders the building state with pending generation and reducer backlog observed", async () => {
    const causes = baseCauses.map((c) =>
      c.cause === "pending_repo_generation" || c.cause === "reducer_backlog" ? { ...c, observed: true } : c,
    );
    const client = clientReturning({
      state: "building",
      scoped: false,
      causes,
      generations: { active: 3, pending: 2, completed: 1, superseded: 0, failed: 0 },
      pending_projection: { outstanding: 7, dead_letter: 0, domains: 1 },
      recent_transitions: [],
    });
    render(<FreshnessCausalityPage client={client} />);
    expect(await screen.findByText(/state: building/)).toBeInTheDocument();
    expect(screen.getByText("2 causes currently observed.")).toBeInTheDocument();
  });

  it("renders the stale state with retraction transitions", async () => {
    const causes = baseCauses.map((c) => (c.cause === "dead_lettered_domain" ? { ...c, observed: true } : c));
    const client = clientReturning({
      state: "stale",
      scoped: false,
      causes,
      generations: { active: 2, pending: 0, completed: 0, superseded: 3, failed: 0 },
      pending_projection: { outstanding: 0, dead_letter: 2, domains: 1 },
      recent_transitions: [
        { status: "superseded", trigger_kind: "push", freshness_hint: "retired", scope_id: "scope-1", generation_id: "gen-old" },
      ],
    });
    render(<FreshnessCausalityPage client={client} />);
    expect(await screen.findByText(/state: stale/)).toBeInTheDocument();
    // Unscoped view shows the raw correlation IDs.
    expect(screen.getByText("scope-1")).toBeInTheDocument();
    expect(screen.getByText("gen-old")).toBeInTheDocument();
  });

  it("renders the tenant-scoped (permission-hidden) view without raw identifiers", async () => {
    const client = clientReturning({
      state: "stale",
      scoped: true,
      causes: baseCauses,
      generations: { active: 1, pending: 0, completed: 0, superseded: 1, failed: 0 },
      pending_projection: { outstanding: 0, dead_letter: 1, domains: 1 },
      recent_transitions: [{ status: "superseded", trigger_kind: "push", freshness_hint: "retired" }],
    });
    render(<FreshnessCausalityPage client={client} />);
    expect(await screen.findByText(/tenant-scoped view/)).toBeInTheDocument();
    expect(screen.getByText("scoped view")).toBeInTheDocument();
    // The Scope and Generation columns are absent in the scoped view.
    expect(screen.queryByText("Scope")).not.toBeInTheDocument();
    expect(screen.queryByText("Generation")).not.toBeInTheDocument();
  });

  it("renders an explicit unavailable state when the endpoint fails", async () => {
    const client = { get: async () => ({ data: null, error: { code: "x", message: "down" }, truth: null }) } as unknown as EshuApiClient;
    render(<FreshnessCausalityPage client={client} />);
    expect(await screen.findByText("Freshness causality unavailable from this source.")).toBeInTheDocument();
  });
});
