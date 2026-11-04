import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";

function finding(number: number): Record<string, unknown> {
  return {
    finding_id: `finding-${number}`,
    cve_id: `CVE-2026-${String(number).padStart(5, "0")}`,
    package_name: "example",
    cvss_score: 8,
  };
}

function renderAt(url: string, get: ReturnType<typeof vi.fn>): void {
  render(
    <MemoryRouter initialEntries={[url]}>
      <VulnerabilitiesPage
        model={{ ...demoModel, source: "live" as const }}
        client={{ get } as unknown as EshuApiClient}
      />
    </MemoryRouter>,
  );
}

describe("reachable finding list service filter", () => {
  it("forwards service_id from the URL to the findings read and names the filter", async () => {
    const get = vi.fn(async () => ({
      data: { findings: [finding(7)], truncated: false },
      error: null,
      truth: null,
    }));
    renderAt("/vulnerabilities?service_id=svc%3Acheckout", get);
    expect(await screen.findByRole("link", { name: "CVE-2026-00007" })).toBeInTheDocument();
    const [path] = get.mock.calls[0] as unknown as [string];
    const params = new URL(path, "http://x").searchParams;
    expect(params.get("service_id")).toBe("svc:checkout");
    expect(params.get("impact_status")).toBe("affected_exact");
    expect(screen.getByText(/service_id/)).toHaveTextContent("svc:checkout");
  });

  it("keeps the filter when switching to the possibly affected list", async () => {
    const get = vi.fn(async (path: string) => ({
      data: {
        findings: [],
        truncated: false,
        detection_profile: new URL(path, "http://x").searchParams.get("profile") ?? "precise",
      },
      error: null,
      truth: null,
    }));
    renderAt("/vulnerabilities?service_id=svc-a", get);
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: "Possibly affected" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    const [path] = get.mock.calls[1] as unknown as [string];
    const params = new URL(path, "http://x").searchParams;
    expect(params.get("impact_status")).toBe("possibly_affected");
    expect(params.get("service_id")).toBe("svc-a");
    expect(params.get("profile")).toBe("comprehensive");
  });

  it("carries a comprehensive profile from the URL into every status read", async () => {
    const get = vi.fn(async () => ({
      data: { findings: [finding(8)], truncated: false, detection_profile: "comprehensive" },
      error: null,
      truth: null,
    }));
    renderAt("/vulnerabilities?service_id=svc-a&profile=comprehensive", get);
    expect(await screen.findByRole("link", { name: "CVE-2026-00008" })).toBeInTheDocument();
    const [path] = get.mock.calls[0] as unknown as [string];
    expect(new URL(path, "http://x").searchParams.get("profile")).toBe("comprehensive");
    expect(screen.getByText(/Detection profile/)).toHaveTextContent("comprehensive");
  });

  it("shows the error, not an empty list or stat tiles, when the filtered read fails", async () => {
    const get = vi.fn(async () => {
      throw new Error("findings unavailable");
    });
    renderAt("/vulnerabilities?service_id=svc-a", get);
    expect(await screen.findByRole("alert")).toHaveTextContent("findings unavailable");
    expect(screen.queryByText(/No affected service proven/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Open findings")).not.toBeInTheDocument();
    expect(screen.queryByText("Findings on page")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Finding register")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Supply-chain impact path")).not.toBeInTheDocument();
  });

  it("treats a profile mismatch on the filtered read as an error", async () => {
    const get = vi.fn(async () => ({
      data: { findings: [], truncated: false, detection_profile: "precise" },
      error: null,
      truth: null,
    }));
    renderAt("/vulnerabilities?service_id=svc-a&profile=comprehensive", get);
    expect(await screen.findByRole("alert")).toHaveTextContent(/precise/);
    expect(screen.queryByText(/No affected service proven/i)).not.toBeInTheDocument();
  });

  it("shows a loading state, not an empty list, while the filtered read is pending", () => {
    const get = vi.fn(() => new Promise<never>(() => undefined));
    renderAt("/vulnerabilities?service_id=svc-a", get);
    expect(screen.getByText(/Loading findings page/)).toBeInTheDocument();
    expect(screen.queryByText(/No affected service proven/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Findings on page")).not.toBeInTheDocument();
  });

  it("says a filtered list needs the live API when there is no client", () => {
    render(
      <MemoryRouter initialEntries={["/vulnerabilities?service_id=svc-a"]}>
        <VulnerabilitiesPage model={{ ...demoModel, source: "live" as const }} />
      </MemoryRouter>,
    );
    expect(screen.getByText(/filtered finding list needs the live API/i)).toBeInTheDocument();
    expect(screen.queryByText(/No affected service proven/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Findings on page")).not.toBeInTheDocument();
  });

  it("shows the empty text only after a filtered page has loaded", async () => {
    const get = vi.fn(async () => ({
      data: { findings: [], truncated: false, detection_profile: "precise" },
      error: null,
      truth: null,
    }));
    renderAt("/vulnerabilities?service_id=svc-a", get);
    expect(await screen.findByText(/No affected service proven/i)).toBeInTheDocument();
  });

  it("does not offer the possibly affected list without a filter", () => {
    const get = vi.fn();
    renderAt("/vulnerabilities", get);
    expect(screen.queryByRole("button", { name: "Possibly affected" })).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
  });
});
