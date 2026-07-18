import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";
import { AdvisoryCatalog } from "./VulnerabilitiesCatalog";

describe("AdvisoryCatalog", () => {
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
});
