import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "../../api/client";
import { demoModel } from "../../console/demoModel";
import { OperationsPage } from "../OperationsPage";

// The live operations board (issue #5137) polls GET /api/v0/status/operations
// independently of the model prop. These tests mirror StatusPage.test.tsx's
// path-switching mock client + small injected pollMs + waitFor convention.
//
// Split out of OperationsPage.test.tsx (#5172 cold-review, 500-line file cap)
// into this component's own directory, since every test here exercises
// OperationsLiveBoard specifically rather than the rest of OperationsPage.
//
// OperationsLiveBoard is React.lazy + Suspense (bundle-budget code-split, see
// OperationsPage.tsx). Every test below renders <OperationsPage>, so the
// FIRST render in this file to reach that Suspense boundary pays a one-time
// dynamic-import module-transform cost before the component (and its first
// poll) can resolve. Locally that's sub-millisecond, but PR #5140 CI reported
// a flake on a constrained runner where it exceeded findByText's default 1s
// timeout (reproduced locally by injecting an artificial delay into the
// dynamic import — the same assertion times out, and passes again once the
// timeout below is applied). Every first suspense-crossing assertion in this
// describe block therefore uses an explicit generous timeout instead of the
// default, so CI resource contention cannot make this file flaky.
const suspenseCrossingTimeout = { timeout: 5000 };

describe("OperationsPage live operations board", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  function operationsWire(overrides: Record<string, unknown> = {}): unknown {
    return {
      version: "test",
      as_of: "2026-06-21T12:00:00Z",
      scoped: false,
      health: { state: "healthy", reasons: [] },
      collectors: [],
      stage_summaries: [],
      queue: {
        outstanding: 0,
        in_flight: 0,
        retrying: 0,
        succeeded: 0,
        dead_letter: 0,
        failed: 0,
        overdue_claims: 0,
      },
      live_activity: [],
      truncated: false,
      limit: 50,
      ...overrides,
    };
  }

  function activityRow(overrides: Record<string, unknown> = {}): unknown {
    return {
      work_item_id: "wi-1",
      stage: "reducer",
      status: "running",
      domain: "repository:checkout-service",
      lease_owner: "reducer-1",
      claim_until: null,
      attempt_count: 1,
      updated_at: null,
      created_at: null,
      age_seconds: 30,
      scope_kind: "repository",
      collector_kind: "git",
      source_system: "github",
      source_key: "repository:r_ea78e8bb",
      source_display: "acme/checkout-service",
      generation_state: "active",
      ...overrides,
    };
  }

  function domainBacklogRow(overrides: Record<string, unknown> = {}): unknown {
    return {
      domain: "repository:checkout-service",
      outstanding: 12,
      pending: 9,
      in_flight: 3,
      blocked: 0,
      retrying: 1,
      dead_letter: 0,
      failed: 0,
      oldest_age: 305,
      ...overrides,
    };
  }

  function opsClient(responses: readonly unknown[]): EshuApiClient {
    let call = 0;
    return {
      get: async (path: string) => {
        if (!path.includes("/status/operations")) throw new Error(`unexpected get ${path}`);
        const idx = Math.min(call, responses.length - 1);
        call += 1;
        return { data: responses[idx], error: null, truth: null };
      },
    } as unknown as EshuApiClient;
  }

  it("polls the mock client and renders a live_activity row, then renders updated rows after the next poll", async () => {
    const first = operationsWire({ live_activity: [activityRow()] });
    const second = operationsWire({
      live_activity: [
        activityRow({
          work_item_id: "wi-2",
          stage: "projector",
          status: "claimed",
          domain: "repository:payments-api",
          lease_owner: "projector-2",
          source_key: "repository:r_1a2b3c4d",
          source_display: "acme/payments-api",
        }),
      ],
    });
    const client = opsClient([first, second]);

    render(<OperationsPage model={demoModel} client={client} pollMs={50} />, {
      wrapper: MemoryRouter,
    });

    // Renders the human-readable source_display, not the raw source_key.
    expect(
      await screen.findByText("acme/checkout-service", {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
    expect(screen.queryByText("repository:r_ea78e8bb")).not.toBeInTheDocument();

    await waitFor(() => expect(screen.getByText("acme/payments-api")).toBeInTheDocument(), {
      timeout: 2000,
    });
    expect(screen.queryByText("acme/checkout-service")).not.toBeInTheDocument();
  });

  it("falls back to the raw source_key when source_display is absent", async () => {
    const client = opsClient([
      operationsWire({
        live_activity: [activityRow({ source_display: null })],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText("repository:r_ea78e8bb", {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
  });

  // issue #5171: the repo label links to the same /repositories/:id/source
  // freshness route the Repositories page uses, for rows a git repository
  // scope's source_key resolves to a repository catalog id.
  it("links a resolvable repo row's label to its repository freshness view", async () => {
    const client = opsClient([operationsWire({ live_activity: [activityRow()] })]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    const link = await screen.findByRole(
      "link",
      { name: "acme/checkout-service" },
      suspenseCrossingTimeout,
    );
    expect(link).toHaveAttribute("href", "/repositories/repository%3Ar_ea78e8bb/source");
  });

  // A row from a non-git-repository scope (e.g. a package-registry collector)
  // carries a source_key that is not a repository catalog id, so it must
  // render as plain text rather than a dead/wrong link (#5171 acceptance
  // criteria).
  it("renders the repo label as plain text when the row's scope is not a repository", async () => {
    const client = opsClient([
      operationsWire({
        live_activity: [
          activityRow({
            scope_kind: "package_registry",
            collector_kind: "package_registry",
            source_key: "pkg:some-package",
            source_display: "some-package",
          }),
        ],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText("some-package", {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "some-package" })).not.toBeInTheDocument();
  });

  it("renders scoped rows safely with an em dash for redacted repo/worker identity", async () => {
    const client = opsClient([
      operationsWire({
        scoped: true,
        live_activity: [activityRow({ lease_owner: null, source_key: null, source_display: null })],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    await screen.findByText("running", {}, suspenseCrossingTimeout);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    // Redacted source_key has no resolvable repository id: no dead link.
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("dims a stale-generation retrying row and badges it, leaving active rows undimmed (#5138)", async () => {
    const client = opsClient([
      operationsWire({
        live_activity: [
          activityRow({ generation_state: "active" }),
          activityRow({
            work_item_id: "wi-2",
            status: "retrying",
            source_key: "repository:r_1a2b3c4d",
            source_display: "acme/payments-api",
            generation_state: "stale",
          }),
        ],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    const staleBadge = await screen.findByText("stale", {}, suspenseCrossingTimeout);
    const staleRow = staleBadge.closest("tr");
    expect(staleRow).toHaveClass("ops-activity-row-stale");
    // The stale row's repo label stays a legible link (dimmed via the
    // existing td.mono color rule, which the inherited-color anchor picks
    // up automatically) rather than losing its link on dimming.
    const staleLink = within(staleRow as HTMLElement).getByRole("link", {
      name: "acme/payments-api",
    });
    expect(staleLink).toHaveAttribute("href", "/repositories/repository%3Ar_1a2b3c4d/source");

    const activeRow = screen.getByText("acme/checkout-service").closest("tr");
    expect(activeRow).not.toHaveClass("ops-activity-row-stale");
    expect(activeRow).not.toBeNull();
    if (activeRow) expect(within(activeRow).queryByText("stale")).not.toBeInTheDocument();
  });

  it("shows the explicit empty state when there is no in-flight work", async () => {
    const client = opsClient([operationsWire()]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText("No in-flight work — pipeline idle", {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
  });

  // domain_backlogs (#5172): the wire field was already fetched into the
  // status/operations response but never rendered. These two tests cover the
  // populated and empty states of the resulting "Top domain backlogs" panel.
  it("renders the top domain backlogs panel with the server's top-N sorted rows", async () => {
    const client = opsClient([
      operationsWire({
        domain_backlogs: [
          domainBacklogRow(),
          domainBacklogRow({
            domain: "package_registry:npm",
            outstanding: 4,
            pending: 4,
            in_flight: 0,
            oldest_age: 40,
          }),
        ],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText("repository:checkout-service", {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
    expect(screen.getByText("package_registry:npm")).toBeInTheDocument();
    expect(screen.getByText("Top domain backlogs")).toBeInTheDocument();
  });

  it("shows the explicit empty state when there is no outstanding domain backlog", async () => {
    const client = opsClient([operationsWire()]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText(
        "No outstanding domain backlog — pipeline idle",
        {},
        suspenseCrossingTimeout,
      ),
    ).toBeInTheDocument();
  });

  // #5172 cold-review P2-3: a domain row whose only pressure is dead-letter/
  // failed work (outstanding, pending, and in_flight all zero) must still
  // render meaningfully -- dead-lettered work needing replay is exactly what
  // this board should surface, not hide behind an all-zero row.
  it("renders a terminal-only row's dead-letter and failed counts, not an all-zero row", async () => {
    const client = opsClient([
      operationsWire({
        domain_backlogs: [
          domainBacklogRow({
            domain: "repository:legacy-importer",
            outstanding: 0,
            pending: 0,
            in_flight: 0,
            retrying: 0,
            dead_letter: 3,
            failed: 2,
            oldest_age: 5400,
          }),
        ],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    const domainCell = await screen.findByText(
      "repository:legacy-importer",
      {},
      suspenseCrossingTimeout,
    );
    const row = domainCell.closest("tr");
    expect(row).not.toBeNull();
    if (row) {
      // Dead-letter and Failed render the real counts, not zeroed out --
      // the row's only signal isn't lost even though outstanding/pending/
      // in_flight are all zero.
      expect(within(row).getByText("3")).toBeInTheDocument();
      expect(within(row).getByText("2")).toBeInTheDocument();
    }
  });

  // #5172 cold-review P2-1: `domain` alone is not a safe React key -- two
  // wire rows with an empty/unparseable domain both clean to the same "—"
  // fallback. This proves both rows render (no row silently dropped/merged
  // by a duplicate key) and pins the no-console-key-warning contract.
  it("renders every row even when domains collide after cleaning to the empty-domain fallback", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const client = opsClient([
      operationsWire({
        domain_backlogs: [
          domainBacklogRow({ domain: "", outstanding: 10, oldest_age: 100 }),
          domainBacklogRow({ domain: "", outstanding: 5, oldest_age: 50 }),
        ],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    const panelTitle = await screen.findByText("Top domain backlogs", {}, suspenseCrossingTimeout);
    const panel = panelTitle.closest("section.panel");
    expect(panel).not.toBeNull();
    if (panel) {
      const scoped = within(panel as HTMLElement);
      expect(scoped.getAllByText("—", { selector: "td" })).toHaveLength(2);
      expect(scoped.getByText("10")).toBeInTheDocument();
      expect(scoped.getByText("5")).toBeInTheDocument();
    }
    const keyWarning = consoleError.mock.calls.some((call) =>
      String(call[0]).includes('unique "key" prop'),
    );
    expect(keyWarning).toBe(false);
    consoleError.mockRestore();
  });

  it("degrades the live board gracefully but keeps rendering the rest of the page when the endpoint is unavailable", async () => {
    const client = {
      get: async () => {
        throw new Error("offline");
      },
    } as unknown as EshuApiClient;
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />, {
      wrapper: MemoryRouter,
    });

    expect(
      await screen.findByText(/Live operations board is unavailable/i, {}, suspenseCrossingTimeout),
    ).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
  });
});
