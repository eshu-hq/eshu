import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AdvisoryCatalog } from "./VulnerabilitiesCatalog";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("AdvisoryCatalog", () => {
  it("replaces retained rows and fences an in-flight request when the source changes", async () => {
    let resolvePage: ((value: unknown) => void) | undefined;
    const client = {
      get: () =>
        new Promise((resolve) => {
          resolvePage = resolve;
        }),
    } as unknown as EshuApiClient;
    const retainedModel: ConsoleModel = {
      ...demoModel,
      source: "live",
      advisories: [
        {
          ...demoModel.advisories[0],
          id: "CVE-PRIVATE-RETAINED",
          packageIds: ["pkg:private/retained@1.0.0"],
        },
      ],
      advisoryCatalogSummary: { count: 1, limit: 50, truncated: false },
      provenance: { ...demoModel.provenance, advisories: "live" },
    };
    const { rerender } = render(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={retainedModel} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Search advisories" }), {
      target: { value: "late" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    rerender(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={demoModel} />
      </MemoryRouter>,
    );
    resolvePage?.({
      data: {
        advisories: [
          {
            advisory_key: "CVE-PRIVATE-LATE",
            cve_id: "CVE-PRIVATE-LATE",
            package_ids: ["pkg:private/late@1.0.0"],
            severity_label: "high",
          },
        ],
        count: 1,
        limit: 50,
        truncated: false,
      },
      error: null,
      truth: null,
    });

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2021-44228" })).toBeInTheDocument(),
    );
    expect(screen.queryByRole("link", { name: "CVE-PRIVATE-RETAINED" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "CVE-PRIVATE-LATE" })).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Search advisories" })).toHaveValue("");
  });

  it("refreshes seeded rows when the connected model changes", async () => {
    const first: ConsoleModel = {
      ...demoModel,
      source: "live",
      advisories: [{ ...demoModel.advisories[0], id: "CVE-2026-FIRST" }],
      advisoryCatalogSummary: { count: 1, limit: 50, truncated: false },
      provenance: { ...demoModel.provenance, advisories: "live" },
    };
    const second: ConsoleModel = {
      ...first,
      advisories: [{ ...demoModel.advisories[0], id: "CVE-2026-SECOND" }],
    };
    const { rerender } = render(
      <MemoryRouter>
        <AdvisoryCatalog model={first} />
      </MemoryRouter>,
    );

    rerender(
      <MemoryRouter>
        <AdvisoryCatalog model={second} />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-SECOND" })).toBeInTheDocument(),
    );
    expect(screen.queryByRole("link", { name: "CVE-2026-FIRST" })).not.toBeInTheDocument();
  });

  it("preserves local browse state across same-content model rerenders", async () => {
    const client = {
      get: async () => ({
        data: {
          advisories: [
            {
              advisory_key: "CVE-2026-FILTERED",
              cve_id: "CVE-2026-FILTERED",
              cvss_score: 8.7,
              severity_label: "high",
            },
          ],
          count: 1,
          limit: 50,
          truncated: false,
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    const model: ConsoleModel = {
      ...demoModel,
      source: "live",
      provenance: { ...demoModel.provenance, advisories: "live" },
    };
    const { rerender } = render(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={model} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Search advisories" }), {
      target: { value: "CVE-2026" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByRole("link", { name: "CVE-2026-FILTERED" });

    rerender(
      <MemoryRouter>
        <AdvisoryCatalog
          client={client}
          model={{
            ...model,
            advisories: model.advisories.map((advisory) => ({
              ...advisory,
              ecosystems: [...advisory.ecosystems],
              packageIds: [...advisory.packageIds],
            })),
            advisoryCatalogSummary:
              model.advisoryCatalogSummary === null ? null : { ...model.advisoryCatalogSummary },
            advisoryCatalogNextCursor:
              model.advisoryCatalogNextCursor === null
                ? null
                : { ...model.advisoryCatalogNextCursor },
          }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("textbox", { name: "Search advisories" })).toHaveValue("CVE-2026");
    expect(screen.getByRole("link", { name: "CVE-2026-FILTERED" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear" })).toBeInTheDocument();
  });

  it("does not present stale rows as matches when a replacement request fails", async () => {
    const client = {
      get: async () => {
        throw new Error("catalog temporarily unavailable");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={demoModel} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Search advisories" }), {
      target: { value: "CVE-2026" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(await screen.findByText("catalog temporarily unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "CVE-2021-44228" })).not.toBeInTheDocument();
  });

  it("preserves bounded filters and truth indicators when refreshing the catalog", async () => {
    const paths: string[] = [];
    const client = {
      get: async (path: string) => {
        paths.push(path);
        return {
          data: {
            advisories: [
              {
                advisory_key: "CVE-2026-0002",
                cve_id: "CVE-2026-0002",
                cvss_score: 8.7,
                ecosystems: ["npm"],
                kev: true,
                package_ids: ["pkg:npm/example@1.0.0"],
                severity_label: "high",
              },
            ],
            count: 1,
            limit: 50,
            truncated: false,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;
    const model: ConsoleModel = {
      ...demoModel,
      source: "live",
      truth: {
        ...demoModel.truth,
        advisories: {
          capability: "supply_chain.advisory_catalog.list",
          freshness: { state: "fresh" },
          level: "exact",
          profile: "production",
        },
      },
      provenance: { ...demoModel.provenance, advisories: "live" },
    };

    render(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={model} />
      </MemoryRouter>,
    );

    expect(screen.getByTitle("Truth: exact")).toBeInTheDocument();
    expect(screen.getByTitle("Freshness: fresh")).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Search advisories" }), {
      target: { value: "CVE-2026" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Severity filter" }), {
      target: { value: "high" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Ecosystem filter" }), {
      target: { value: "npm" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: "KEV only" }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "CVE-2026-0002" })).toBeInTheDocument(),
    );
    expect(paths).toHaveLength(1);
    const request = new URL(paths[0], "http://eshu.invalid");
    expect(request.searchParams.get("limit")).toBe("50");
    expect(request.searchParams.get("q")).toBe("CVE-2026");
    expect(request.searchParams.get("severity")).toBe("high");
    expect(request.searchParams.get("ecosystem")).toBe("npm");
    expect(request.searchParams.get("kev")).toBe("true");
  });

  it("keeps cleared filters authoritative when an older request completes late", async () => {
    let resolvePage: ((value: unknown) => void) | undefined;
    const client = {
      get: () =>
        new Promise((resolve) => {
          resolvePage = resolve;
        }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <AdvisoryCatalog client={client} model={demoModel} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Search advisories" }), {
      target: { value: "CVE-2026" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    fireEvent.click(await screen.findByRole("button", { name: "Clear" }));

    resolvePage?.({
      data: {
        advisories: [
          {
            advisory_key: "CVE-2026-LATE",
            cve_id: "CVE-2026-LATE",
            cvss_score: 8.7,
            severity_label: "high",
          },
        ],
        count: 1,
        limit: 50,
        truncated: false,
      },
      error: null,
      truth: null,
    });

    await waitFor(() =>
      expect(screen.queryByRole("link", { name: "CVE-2026-LATE" })).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: "CVE-2021-44228" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Search advisories" })).toHaveValue("");
  });
});
