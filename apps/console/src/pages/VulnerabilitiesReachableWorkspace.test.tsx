import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";

function groupedPage(): Record<string, unknown> {
  return {
    buckets: [
      {
        dimension: "service_id",
        value: "svc:checkout",
        unattributed: false,
        count: 1,
        by_impact_status: { affected_exact: 1 },
        by_severity: { high: 1 },
        max_priority_score: 80,
        completeness: "complete",
      },
    ],
    count: 1,
    limit: 50,
    offset: 0,
    group_by: "service_id",
    detection_profile: "precise",
    distinct_findings: 1,
    truncated: false,
    next_offset: null,
  };
}

function clientForWorkspace(): { client: EshuApiClient; get: ReturnType<typeof vi.fn> } {
  const get = vi.fn(async (path: string) => {
    if (path.includes("/impact/inventory")) {
      return { data: groupedPage(), error: null, truth: null };
    }
    return {
      data: {
        findings: [
          {
            finding_id: "finding-7",
            cve_id: "CVE-2026-00007",
            package_name: "example",
            cvss_score: 8,
          },
        ],
        truncated: false,
        detection_profile: "precise",
      },
      error: null,
      truth: null,
    };
  });
  return { client: { get } as unknown as EshuApiClient, get };
}

describe("reachable workspace view switching", () => {
  it("opens the filtered finding list when a group link is clicked", async () => {
    const { client, get } = clientForWorkspace();
    render(
      <MemoryRouter initialEntries={["/vulnerabilities"]}>
        <VulnerabilitiesPage model={{ ...demoModel, source: "live" as const }} client={client} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "By service" }));
    fireEvent.click(await screen.findByRole("link", { name: "svc:checkout" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Filtered to service_id: svc:checkout.",
    );
    expect(screen.getByRole("button", { name: "Finding list" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(await screen.findByRole("link", { name: "CVE-2026-00007" })).toBeInTheDocument();
    const findingsCall = get.mock.calls
      .map(([path]) => new URL(path as string, "http://x"))
      .find((url) => url.pathname.includes("/impact/findings"));
    expect(findingsCall?.searchParams.get("service_id")).toBe("svc:checkout");
  });

  it("keeps the grouped view when the user picks it while a filter is set", async () => {
    const { client } = clientForWorkspace();
    render(
      <MemoryRouter initialEntries={["/vulnerabilities?service_id=svc%3Acheckout"]}>
        <VulnerabilitiesPage model={{ ...demoModel, source: "live" as const }} client={client} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "By service" }));
    expect(screen.getByRole("button", { name: "By service" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(await screen.findByRole("link", { name: "svc:checkout" })).toBeInTheDocument();
  });
});
