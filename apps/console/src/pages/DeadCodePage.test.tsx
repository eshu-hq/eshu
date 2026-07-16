import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { DeadCodePage } from "./DeadCodePage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function envelope(results: readonly Record<string, unknown>[]) {
  return {
    data: {
      analysis: { dead_code_language_maturity: { typescript: "experimental" } },
      limit: 100,
      results,
      truncated: true,
    },
    error: null,
    truth: {
      capability: "code_quality.dead_code",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production",
    },
  };
}

describe("DeadCodePage", () => {
  it("renders the dedicated dead-code workbench from finding rows", () => {
    renderDeadCode(<DeadCodePage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "Dead code" })).toBeInTheDocument();
    expect(screen.getByText(/POST \/api\/v0\/code\/dead-code/)).toBeInTheDocument();
    expect(screen.getByText(/Select a location to open the source file/)).toBeInTheDocument();
    expect(screen.getByLabelText("Dead-code workbench")).toBeInTheDocument();
    expect(screen.getByText(/Grouped by repository/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /All kinds/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Any" })).toBeInTheDocument();
    expect(screen.getByText("Unreferenced symbol legacyDiscount")).toBeInTheDocument();
    expect(screen.queryByText("CVE-2024-0001 reachable in prod image")).not.toBeInTheDocument();
  });

  it("filters candidates by analyzer classification", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        ...demoModel.findings.map((finding) =>
          finding.id === "d2" ? { ...finding, detail: "src/discounts.ts · unused" } : finding,
        ),
        {
          id: "d3",
          type: "Dead code",
          entity: "payments-service",
          title: "Unreferenced symbol oldWebhook",
          detail: "src/webhooks.ts · ambiguous",
          truth: "derived",
        },
      ],
    };
    renderDeadCode(<DeadCodePage model={model} />);

    fireEvent.click(screen.getByRole("button", { name: /unused/ }));

    expect(screen.getByText("Unreferenced symbol legacyDiscount")).toBeInTheDocument();
    expect(screen.queryByText("Unreferenced symbol oldWebhook")).not.toBeInTheDocument();
  });

  it("renders an honest empty state when no dead-code candidates exist", () => {
    const empty: ConsoleModel = {
      ...demoModel,
      findings: demoModel.findings.filter((finding) => finding.type !== "Dead code"),
    };

    renderDeadCode(<DeadCodePage model={empty} />);

    expect(screen.getByText("No dead-code candidates from this source.")).toBeInTheDocument();
  });

  it("loads the dedicated live dead-code scan with filters", async () => {
    const get = vi.fn(async () => ({
      data: {
        repositories: [{ id: "repository:r1", name: "svc-platform" }],
      },
      error: null,
      truth: null,
    }));
    const post = vi.fn(async (_path: string, body: Record<string, unknown>) =>
      envelope([
        {
          classification: "unused",
          entity_id: body.candidate_kind === "Trait" ? "trait:t1" : "function:f1",
          file_path: "server/routes.ts",
          labels: [body.candidate_kind === "Trait" ? "Trait" : "Function"],
          language: "typescript",
          name: body.candidate_kind === "Trait" ? "UnusedTrait" : "unusedRoute",
          repo_id: "repository:r1",
          start_line: 10,
        },
      ]),
    );
    const client = { get, post } as unknown as EshuApiClient;

    renderDeadCode(<DeadCodePage client={client} model={{ ...demoModel, findings: [] }} />);

    await waitFor(() =>
      expect(screen.getByText("Unreferenced symbol unusedRoute")).toBeInTheDocument(),
    );
    expect(screen.getByText("svc-platform")).toBeInTheDocument();
    expect(screen.queryByText("repository:r1")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "server/routes.ts:10" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar1/source?path=server%2Froutes.ts&lineStart=10",
    );
    expect(screen.getByRole("link", { name: "Open graph" })).toHaveAttribute(
      "href",
      "/code-graph?candidate=function%3Af1",
    );
    expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", { limit: 100 });

    fireEvent.change(screen.getByLabelText("Repository selector"), {
      target: { value: "repository:r1" },
    });
    fireEvent.change(screen.getByLabelText("Language selector"), {
      target: { value: "typescript" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        language: "typescript",
        limit: 100,
        repo_id: "repository:r1",
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: /trait/i }));
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        candidate_kind: "Trait",
        language: "typescript",
        limit: 100,
        repo_id: "repository:r1",
      }),
    );
    expect(await screen.findByText("Unreferenced symbol UnusedTrait")).toBeInTheDocument();
    expect(screen.getByText(/100 candidate scan/)).toBeInTheDocument();
  });
});

function renderDeadCode(element: ReactElement): ReturnType<typeof render> {
  return render(<MemoryRouter>{element}</MemoryRouter>);
}
