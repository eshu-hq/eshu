import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "../api/client";
import type { VulnRow } from "../api/eshuConsoleLive";
import { demoModel } from "../console/demoModel";
import { VulnerabilitiesPage } from "./VulnerabilitiesPage";

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

  it("reaches all 268 exact findings without retaining earlier pages", async () => {
    const first = Array.from({ length: 100 }, (_, index) => row(index + 1));
    const get = vi.fn(async (path: string) => {
      const after = new URLSearchParams(path.split("?")[1]).get("after_finding_id");
      const numbers =
        after === "finding-100"
          ? Array.from({ length: 100 }, (_, index) => index + 101)
          : Array.from({ length: 68 }, (_, index) => index + 201);
      return {
        data: {
          findings: numbers.map(finding),
          count: numbers.length,
          limit: 100,
          truncated: after === "finding-100",
          ...(after === "finding-100" ? { next_cursor: { after_finding_id: "finding-200" } } : {}),
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
  });
});
