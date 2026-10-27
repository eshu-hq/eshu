import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import type { EshuApiClient } from "../api/client";
import type { VulnRow } from "../api/eshuConsoleLive";
import { demoModel } from "../console/demoModel";

function row(number: number): VulnRow {
  return {
    findingId: `finding-${number}`,
    id: `CVE-2026-${String(number).padStart(5, "0")}`,
    package: "example",
    severity: "high",
    cvss: 8,
    kev: false,
    fixedVersion: null,
    services: [],
  };
}

function finding(number: number): Record<string, unknown> {
  return {
    finding_id: `finding-${number}`,
    cve_id: `CVE-2026-${String(number).padStart(5, "0")}`,
    package_name: "example",
    cvss_score: 8,
    known_exploited: false,
  };
}

describe("reachable vulnerability paging", () => {
  it("renders source, GHSA alias, KEV, and zero EPSS as badges", () => {
    const enriched = {
      ...row(1),
      ghsaIds: ["GHSA-aaaa-bbbb-cccc"],
      sources: ["OSV"],
      epssProbability: 0,
      kev: true,
    };
    const model = { ...demoModel, vulnerabilities: [enriched] };
    render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} />
      </MemoryRouter>,
    );
    const badges = [...document.querySelectorAll(".badge")].map((badge) => badge.textContent);
    expect(badges).toEqual(
      expect.arrayContaining(["GHSA-aaaa-bbbb-cccc", "OSV", "EPSS 0.00%", "KEV"]),
    );
  });

  it("keeps exact and derived findings separate", () => {
    const exact = [row(1)];
    const derived = [row(2)];
    const model = {
      ...demoModel,
      source: "live" as const,
      vulnerabilities: [...exact, ...derived],
      vulnerabilityPages: {
        affected_exact: {
          rows: exact,
          count: 1,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
        affected_derived: {
          rows: derived,
          count: 1,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
      },
    };
    render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "CVE-2026-00001" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "CVE-2026-00002" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Derived impact" }));
    expect(screen.getByRole("link", { name: "CVE-2026-00002" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "CVE-2026-00001" })).not.toBeInTheDocument();
  });

  it("keeps the current page and reports a failed next request", async () => {
    const first = [row(1)];
    const model = {
      ...demoModel,
      source: "live" as const,
      vulnerabilities: first,
      vulnerabilityPages: {
        affected_exact: {
          rows: first,
          count: 1,
          limit: 100,
          truncated: true,
          nextCursor: "finding-1",
          truth: null,
        },
        affected_derived: {
          rows: [],
          count: 0,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
      },
    };
    const get = vi.fn(async () => {
      throw new Error("read unavailable");
    });
    render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} client={{ get } as unknown as EshuApiClient} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Next findings page" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("read unavailable"));
    expect(screen.getByRole("link", { name: "CVE-2026-00001" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next findings page" })).toBeEnabled();
  });

  it("discards a late exact response after switching to derived and aborts on unmount", async () => {
    const exact = [row(1)];
    const derived = [row(2)];
    const model = {
      ...demoModel,
      source: "live" as const,
      vulnerabilities: [...exact, ...derived],
      vulnerabilityPages: {
        affected_exact: {
          rows: exact,
          count: 1,
          limit: 100,
          truncated: true,
          nextCursor: "finding-1",
          truth: null,
        },
        affected_derived: {
          rows: derived,
          count: 1,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
      },
    };
    let release!: (value: unknown) => void;
    const get = vi.fn(
      () =>
        new Promise<unknown>((resolve) => {
          release = resolve;
        }),
    );
    const client = { get } as unknown as EshuApiClient;
    const rendered = render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} client={client} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Next findings page" }));
    const signal = (get.mock.calls[0] as unknown as [string, { signal: AbortSignal }])[1].signal;
    fireEvent.click(screen.getByRole("button", { name: "Derived impact" }));
    expect(signal.aborted).toBe(true);
    await act(async () =>
      release({
        data: { findings: [finding(3)], truncated: false },
        error: null,
        truth: null,
      }),
    );
    expect(screen.getByRole("link", { name: "CVE-2026-00002" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "CVE-2026-00003" })).not.toBeInTheDocument();
    rendered.unmount();
  });

  it("aborts a pending page request when the view unmounts", () => {
    const first = [row(1)];
    const model = {
      ...demoModel,
      source: "live" as const,
      vulnerabilities: first,
      vulnerabilityPages: {
        affected_exact: {
          rows: first,
          count: 1,
          limit: 100,
          truncated: true,
          nextCursor: "finding-1",
          truth: null,
        },
        affected_derived: {
          rows: [],
          count: 0,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
      },
    };
    const get = vi.fn(() => new Promise<unknown>(() => {}));
    const rendered = render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} client={{ get } as unknown as EshuApiClient} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Next findings page" }));
    const signal = (get.mock.calls[0] as unknown as [string, { signal: AbortSignal }])[1].signal;
    expect(signal.aborted).toBe(false);
    rendered.unmount();
    expect(signal.aborted).toBe(true);
  });

  it("reaches all 268 exact findings without retaining earlier pages", async () => {
    const first = Array.from({ length: 100 }, (_, index) => row(index + 1));
    const get = vi.fn(async (path: string) => {
      const after = new URLSearchParams(path.split("?")[1]).get("after_finding_id");
      const numbers =
        after === "finding-100"
          ? Array.from({ length: 100 }, (_, index) => index + 101)
          : after === "finding-200"
            ? Array.from({ length: 68 }, (_, index) => index + 201)
            : Array.from({ length: 100 }, (_, index) => index + 1);
      return {
        data: {
          findings: numbers.map(finding),
          count: numbers.length,
          limit: 100,
          truncated: after !== "finding-200",
          ...(after !== "finding-200"
            ? {
                next_cursor: {
                  after_finding_id: after === "finding-100" ? "finding-200" : "finding-100",
                },
              }
            : {}),
        },
        error: null,
        truth: null,
      };
    });
    const client = { get } as unknown as EshuApiClient;
    const model = {
      ...demoModel,
      source: "live" as const,
      vulnerabilities: first,
      vulnerabilityPages: {
        affected_exact: {
          rows: first,
          count: 100,
          limit: 100,
          truncated: true,
          nextCursor: "finding-100",
          truth: null,
        },
        affected_derived: {
          rows: [],
          count: 0,
          limit: 100,
          truncated: false,
          nextCursor: null,
          truth: null,
        },
      },
    };
    render(
      <MemoryRouter>
        <VulnerabilitiesPage model={model} client={client} />
      </MemoryRouter>,
    );

    expect(document.querySelector(".supply-chain-register-scroll")).toHaveAttribute(
      "tabindex",
      "0",
    );
    expect(document.querySelectorAll("[data-vulnerability-finding-id]")).toHaveLength(100);
    fireEvent.click(screen.getByRole("button", { name: "Next findings page" }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-00101" })).toBeInTheDocument(),
    );
    expect(document.querySelectorAll("[data-vulnerability-finding-id]")).toHaveLength(100);
    fireEvent.click(screen.getByRole("button", { name: "Next findings page" }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-00268" })).toBeInTheDocument(),
    );
    expect(document.querySelectorAll("[data-vulnerability-finding-id]")).toHaveLength(68);
    expect(screen.queryByRole("link", { name: "CVE-2026-00001" })).not.toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("button", { name: "Previous findings page" }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-00101" })).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole("button", { name: "Previous findings page" }));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-00001" })).toBeInTheDocument(),
    );
    expect(get).toHaveBeenCalledTimes(4);
    expect(document.querySelectorAll("[data-vulnerability-finding-id]")).toHaveLength(100);
  });
});
