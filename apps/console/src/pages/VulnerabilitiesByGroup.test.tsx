import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { VulnerabilitiesByGroup } from "./VulnerabilitiesByGroup";
import type { EshuApiClient } from "../api/client";

type Wire = Record<string, unknown>;

function wireBucket(overrides: Wire): Wire {
  return {
    dimension: "service_id",
    value: "svc",
    unattributed: false,
    count: 1,
    by_impact_status: { affected_exact: 1 },
    by_severity: { high: 1 },
    max_priority_score: 10,
    completeness: "complete",
    ...overrides,
  };
}

function wirePage(buckets: readonly Wire[], overrides: Wire = {}): Wire {
  return {
    buckets,
    count: buckets.length,
    limit: 50,
    offset: 0,
    group_by: "service_id",
    detection_profile: "precise",
    distinct_findings: buckets.reduce((sum, item) => sum + (item.count as number), 0),
    truncated: false,
    next_offset: null,
    ...overrides,
  };
}

function clientFor(respond: (params: URLSearchParams) => Wire): {
  client: EshuApiClient;
  get: ReturnType<typeof vi.fn>;
} {
  const get = vi.fn(async (path: string) => ({
    data: respond(new URL(path, "http://x").searchParams),
    error: null,
    truth: null,
  }));
  return { client: { get } as unknown as EshuApiClient, get };
}

function renderView(client?: EshuApiClient): void {
  render(
    <MemoryRouter>
      <VulnerabilitiesByGroup client={client} />
    </MemoryRouter>,
  );
}

function rowFor(name: string): HTMLElement {
  const row = screen.getByText(name).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("VulnerabilitiesByGroup", () => {
  it("says it needs the live API when no client is available", () => {
    renderView(undefined);
    expect(screen.getByText(/needs the live API/i)).toBeInTheDocument();
  });

  it("sorts services by highest priority, shows status and severity counts, and links to the filtered list", async () => {
    const { client } = clientFor(() =>
      wirePage([
        wireBucket({ value: "low-svc", count: 9, max_priority_score: 20 }),
        wireBucket({
          value: "checkout",
          count: 3,
          by_impact_status: { affected_exact: 2, affected_derived: 1 },
          by_severity: { critical: 1, high: 2 },
          max_priority_score: 95,
        }),
      ]),
    );
    renderView(client);
    const link = await screen.findByRole("link", { name: "checkout" });
    expect(link.getAttribute("href")).toBe("/vulnerabilities?service_id=checkout");
    const rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]).getByText("checkout")).toBeInTheDocument();
    expect(within(rows[1]).getByText("low-svc")).toBeInTheDocument();
    const row = rowFor("checkout");
    expect(within(row).getByText("exact 2")).toBeInTheDocument();
    expect(within(row).getByText("derived 1")).toBeInTheDocument();
    expect(within(row).getByText("critical 1")).toBeInTheDocument();
    expect(within(row).getByText("high 2")).toBeInTheDocument();
  });

  it("renders the unattributed bucket as 'No service attributed', never as a link or as complete", async () => {
    const { client } = clientFor(() =>
      wirePage([
        wireBucket({
          value: "",
          unattributed: true,
          count: 4,
          completeness: "complete",
          max_priority_score: 99,
        }),
      ]),
    );
    renderView(client);
    const name = await screen.findByText("No service attributed");
    const row = name.closest("tr") as HTMLElement;
    expect(within(row).queryByRole("link")).not.toBeInTheDocument();
    expect(row.getAttribute("data-completeness")).toBe("unattributed");
    expect(row.getAttribute("data-unattributed")).toBe("true");
    expect(within(row).queryByText("Complete")).not.toBeInTheDocument();
    expect(within(row).getByText(/cannot be opened as a service filter/i)).toBeInTheDocument();
  });

  it("shows a possibly-affected-only service only under the comprehensive profile", async () => {
    const { client, get } = clientFor((params) =>
      params.get("profile") === "comprehensive"
        ? wirePage(
            [
              wireBucket({ value: "exact-svc", count: 1 }),
              wireBucket({
                value: "possible-only-svc",
                count: 2,
                by_impact_status: { possibly_affected: 2 },
                max_priority_score: 5,
              }),
            ],
            { detection_profile: "comprehensive" },
          )
        : wirePage([wireBucket({ value: "exact-svc", count: 1 })]),
    );
    renderView(client);
    await screen.findByRole("link", { name: "exact-svc" });
    expect(screen.queryByText("possible-only-svc")).not.toBeInTheDocument();
    expect(screen.getByText(/Detection profile/)).toHaveTextContent("precise");
    expect((get.mock.calls[0] as unknown as [string])[0]).toContain("profile=precise");

    fireEvent.click(screen.getByRole("checkbox", { name: /include possibly affected/i }));
    expect(await screen.findByRole("link", { name: "possible-only-svc" })).toBeInTheDocument();
    expect(
      within(rowFor("possible-only-svc")).getByText("possibly affected 2"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Detection profile/)).toHaveTextContent("comprehensive");
    expect(get.mock.calls.at(-1)?.[0]).toContain("profile=comprehensive");
  });

  it("shows an error, not a clean table, when the server answers on another profile", async () => {
    const { client } = clientFor((params) =>
      params.get("profile") === "comprehensive"
        ? wirePage([wireBucket({ value: "should-not-show" })], { detection_profile: "precise" })
        : wirePage([wireBucket({ value: "svc" })]),
    );
    renderView(client);
    await screen.findByRole("link", { name: "svc" });
    fireEvent.click(screen.getByRole("checkbox", { name: /include possibly affected/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/requested comprehensive/i);
    expect(screen.queryByText("should-not-show")).not.toBeInTheDocument();
    expect(screen.queryByText(/No groups returned/i)).not.toBeInTheDocument();
  });

  it("carries the comprehensive profile into the group link so the list matches the counts", async () => {
    const { client } = clientFor((params) =>
      wirePage([wireBucket({ value: "svc" })], { detection_profile: params.get("profile") }),
    );
    renderView(client);
    await screen.findByRole("link", { name: "svc" });
    fireEvent.click(screen.getByRole("checkbox", { name: /include possibly affected/i }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "svc" }).getAttribute("href")).toBe(
        "/vulnerabilities?service_id=svc&profile=comprehensive",
      ),
    );
  });

  it("never shows the empty text when there is no client or the read has not loaded", () => {
    renderView(undefined);
    expect(screen.queryByText(/No groups returned/i)).not.toBeInTheDocument();
  });

  it("explains overlapping group counts when they exceed distinct findings", async () => {
    const { client } = clientFor(() =>
      wirePage([wireBucket({ value: "a", count: 3 }), wireBucket({ value: "b", count: 2 })], {
        distinct_findings: 3,
      }),
    );
    renderView(client);
    const note = await screen.findByText(/counted in each service/i);
    expect(note).toHaveTextContent("5");
    expect(note).toHaveTextContent("3 distinct findings");
  });

  it("omits the overlap note when counts do not exceed distinct findings", async () => {
    const { client } = clientFor(() => wirePage([wireBucket({ value: "a", count: 3 })]));
    renderView(client);
    await screen.findByRole("link", { name: "a" });
    expect(screen.queryByText(/counted in each/i)).not.toBeInTheDocument();
  });

  it("badges every completeness value and marks the unsafe ones", async () => {
    const { client } = clientFor(() =>
      wirePage([
        wireBucket({ value: "s-complete", completeness: "complete" }),
        wireBucket({ value: "s-partial", completeness: "partial", missing_evidence: ["deploy"] }),
        wireBucket({ value: "s-unavailable", completeness: "read_model_unavailable" }),
        wireBucket({ value: "", unattributed: true, completeness: "unattributed" }),
      ]),
    );
    renderView(client);
    await screen.findByRole("link", { name: "s-complete" });
    expect(within(rowFor("s-complete")).getByText("Complete")).toBeInTheDocument();
    expect(within(rowFor("s-partial")).getByText("Partial")).toBeInTheDocument();
    expect(within(rowFor("s-partial")).getByText(/Missing: deploy/)).toBeInTheDocument();
    expect(within(rowFor("s-unavailable")).getByText("Read model unavailable")).toBeInTheDocument();
    const unattributed = rowFor("No service attributed");
    expect(within(unattributed).getByText("Not attributed")).toBeInTheDocument();
    for (const [name, state] of [
      ["s-complete", "complete"],
      ["s-partial", "partial"],
      ["s-unavailable", "read_model_unavailable"],
    ] as const) {
      expect(rowFor(name).getAttribute("data-completeness")).toBe(state);
    }
  });

  it("renders an empty page as an explicit empty state, not an error", async () => {
    const { client } = clientFor(() => wirePage([]));
    renderView(client);
    expect(await screen.findByText(/No groups returned/i)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("surfaces a failed read as an alert instead of an empty table", async () => {
    const get = vi.fn(async () => {
      throw new Error("inventory unavailable");
    });
    renderView({ get } as unknown as EshuApiClient);
    expect(await screen.findByRole("alert")).toHaveTextContent("inventory unavailable");
    expect(screen.queryByText(/No groups returned/i)).not.toBeInTheDocument();
  });

  it("switches the group and links workload and environment groups to their filters", async () => {
    const { client, get } = clientFor((params) => {
      const groupBy = params.get("group_by");
      if (groupBy === "environment") {
        return wirePage(
          [
            wireBucket({ dimension: "environment", value: "prod" }),
            wireBucket({ dimension: "environment", value: "", unattributed: true, count: 2 }),
          ],
          { group_by: "environment" },
        );
      }
      if (groupBy === "workload_id") {
        return wirePage([wireBucket({ dimension: "workload_id", value: "wl-1" })], {
          group_by: "workload_id",
        });
      }
      return wirePage([wireBucket({})]);
    });
    renderView(client);
    await screen.findByRole("link", { name: "svc" });
    fireEvent.click(screen.getByRole("button", { name: "Workload" }));
    const workload = await screen.findByRole("link", { name: "wl-1" });
    expect(workload.getAttribute("href")).toBe("/vulnerabilities?workload_id=wl-1");
    fireEvent.click(screen.getByRole("button", { name: "Environment" }));
    const env = await screen.findByRole("link", { name: "prod" });
    expect(env.getAttribute("href")).toBe("/vulnerabilities?environment=prod");
    expect(screen.getByText("No current deploy evidence")).toBeInTheDocument();
    expect(get.mock.calls.at(-1)?.[0]).toContain("group_by=environment");
  });

  it("pages with the server next_offset", async () => {
    const { client, get } = clientFor((params) =>
      params.get("offset") === "50"
        ? wirePage([wireBucket({ value: "page-two" })], { offset: 50 })
        : wirePage([wireBucket({ value: "page-one" })], { truncated: true, next_offset: 50 }),
    );
    renderView(client);
    await screen.findByRole("link", { name: "page-one" });
    fireEvent.click(screen.getByRole("button", { name: "Next groups page" }));
    await screen.findByRole("link", { name: "page-two" });
    expect(get.mock.calls.at(-1)?.[0]).toContain("offset=50");
    fireEvent.click(screen.getByRole("button", { name: "Previous groups page" }));
    await screen.findByRole("link", { name: "page-one" });
  });
});
