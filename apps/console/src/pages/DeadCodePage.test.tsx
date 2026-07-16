import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, type ReactElement } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { vi } from "vitest";

import { DeadCodePage } from "./DeadCodePage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";
import { readyRepositoryCatalog } from "../repositoryCatalogLifecycle";

function envelope(
  results: readonly Record<string, unknown>[],
  truncation: {
    readonly candidateScanTruncated?: boolean;
    readonly displayTruncated?: boolean;
  } = {},
) {
  return {
    data: {
      analysis: {
        dead_code_language_maturity: { go: "derived", typescript: "experimental" },
      },
      candidate_scan_truncated: truncation.candidateScanTruncated === true,
      display_truncated: truncation.displayTruncated === true,
      limit: 100,
      results,
      truncated: truncation.candidateScanTruncated === true || truncation.displayTruncated === true,
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
    const get = vi.fn();
    const post = vi.fn(async (_path: string, body: Record<string, unknown>) =>
      envelope(
        [
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
        ],
        { candidateScanTruncated: true },
      ),
    );
    const client = { get, post } as unknown as EshuApiClient;

    renderDeadCode(
      <DeadCodePage
        client={client}
        model={{ ...demoModel, findings: [] }}
        repositoryCatalog={readyRepositoryCatalog([
          {
            groupKey: "",
            groupKind: "",
            groupReason: "",
            groupSource: "",
            groupTruth: "",
            id: "repository:r1",
            isDependency: false,
            name: "svc-platform",
            remoteUrl: "",
            repoSlug: "acme/svc-platform",
          },
        ])}
      />,
    );

    await waitFor(() =>
      expect(screen.getByText("Unreferenced symbol unusedRoute")).toBeInTheDocument(),
    );
    expect(screen.getByText("svc-platform")).toBeInTheDocument();
    expect(screen.queryByText("repository:r1")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Find dead-code candidate"), {
      target: { value: "svc-platform" },
    });
    expect(screen.getByText("Unreferenced symbol unusedRoute")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Find dead-code candidate"), {
      target: { value: "" },
    });
    expect(get).not.toHaveBeenCalled();
    expect(screen.getByText("Repositories represented")).toBeInTheDocument();
    expect(screen.queryByText("Repos affected")).not.toBeInTheDocument();
    expect(
      screen.getByText(/candidate scan window incomplete/i, {
        selector: ".dead-code-scan-status",
      }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Repository selector")).toHaveAttribute(
      "list",
      "dead-code-repository-options",
    );
    expect(screen.getByLabelText("Language selector")).toHaveAttribute(
      "list",
      "dead-code-language-options",
    );
    expect(
      document.querySelector('#dead-code-repository-options option[value="repository:r1"]'),
    ).toHaveTextContent("svc-platform · repository:r1");
    expect(document.querySelector('#dead-code-language-options option[value="go"]')).not.toBeNull();
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
    expect(screen.getByRole("button", { name: /trait/i })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "All kinds" }));
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        language: "typescript",
        limit: 100,
        repo_id: "repository:r1",
      }),
    );
  });

  it("reuses one live scan during StrictMode effect replay", async () => {
    const post = vi.fn(async () =>
      envelope([
        {
          classification: "unused",
          entity_id: "function:f1",
          file_path: "server/routes.ts",
          labels: ["Function"],
          language: "typescript",
          name: "unusedRoute",
          repo_id: "repository:r1",
          start_line: 10,
        },
      ]),
    );
    const client = { get: vi.fn(), post } as unknown as EshuApiClient;

    renderDeadCode(
      <StrictMode>
        <DeadCodePage client={client} model={{ ...demoModel, findings: [] }} />
      </StrictMode>,
    );

    await screen.findByText("Unreferenced symbol unusedRoute");
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("starts a fresh scan when settled filters are applied again", async () => {
    const post = vi.fn(async () =>
      envelope([
        {
          classification: "unused",
          entity_id: `function:f${post.mock.calls.length}`,
          file_path: "server/routes.ts",
          labels: ["Function"],
          language: "typescript",
          name: post.mock.calls.length === 1 ? "firstCandidate" : "refreshedCandidate",
          repo_id: "repository:r1",
          start_line: 10,
        },
      ]),
    );
    const client = { get: vi.fn(), post } as unknown as EshuApiClient;

    renderDeadCode(<DeadCodePage client={client} model={{ ...demoModel, findings: [] }} />);

    await screen.findByText("Unreferenced symbol firstCandidate");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(await screen.findByText("Unreferenced symbol refreshedCandidate")).toBeInTheDocument();
    expect(post).toHaveBeenCalledTimes(2);
  });

  it("synchronizes the local query when applying live scopes", async () => {
    const post = vi.fn(async () =>
      envelope([
        {
          classification: "unused",
          entity_id: "function:f1",
          file_path: "server/routes.ts",
          labels: ["Function"],
          language: "typescript",
          name: "staleCandidate",
          repo_id: "repository:r1",
          start_line: 10,
        },
      ]),
    );
    const client = { get: vi.fn(), post } as unknown as EshuApiClient;

    renderDeadCode(
      <>
        <DeadCodePage client={client} model={{ ...demoModel, findings: [] }} />
        <LocationSearch />
      </>,
      ["/dead-code?q=stale"],
    );

    await screen.findByText("Unreferenced symbol staleCandidate");
    fireEvent.change(screen.getByLabelText("Find dead-code candidate"), {
      target: { value: "" },
    });
    fireEvent.change(screen.getByLabelText("Language selector"), {
      target: { value: "typescript" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(screen.getByTestId("location-search")).toHaveTextContent("?language=typescript"),
    );
  });

  it("opens an actionable current-window repository breakdown", async () => {
    const post = vi.fn(async () =>
      envelope(
        [
          {
            classification: "unused",
            end_line: 14,
            entity_id: "function:f1",
            file_path: "server/routes.ts",
            labels: ["Function"],
            language: "typescript",
            name: "unusedRoute",
            repo_id: "repository:r1",
            start_line: 10,
          },
        ],
        { candidateScanTruncated: true },
      ),
    );
    const client = { get: vi.fn(), post } as unknown as EshuApiClient;

    renderDeadCode(
      <DeadCodePage
        client={client}
        model={{ ...demoModel, findings: [] }}
        repositoryCatalog={readyRepositoryCatalog([
          {
            groupKey: "",
            groupKind: "",
            groupReason: "",
            groupSource: "",
            groupTruth: "",
            id: "repository:r1",
            isDependency: false,
            name: "svc-platform",
            remoteUrl: "",
            repoSlug: "acme/svc-platform",
          },
        ])}
      />,
    );

    await screen.findByText("Unreferenced symbol unusedRoute");
    fireEvent.click(screen.getByRole("button", { name: "Show repository breakdown" }));

    expect(screen.getByRole("region", { name: "Repository breakdown" })).toBeInTheDocument();
    expect(screen.getByText("repository:r1")).toBeInTheDocument();
    expect(screen.getByText("5 LOC")).toBeInTheDocument();
    const filteredLink = screen.getByRole("link", { name: "View candidates" });
    expect(filteredLink).toHaveAttribute("href", "/dead-code?repo_id=repository%3Ar1");

    fireEvent.click(filteredLink);
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        limit: 100,
        repo_id: "repository:r1",
      }),
    );
  });

  it("preserves active server filters when drilling into a repository", async () => {
    const post = vi.fn(async () =>
      envelope([
        {
          classification: "unused",
          entity_id: "function:f1",
          file_path: "server/routes.ts",
          labels: ["Function"],
          language: "typescript",
          name: "unusedRoute",
          repo_id: "repository:r1",
          start_line: 10,
        },
      ]),
    );
    const client = { get: vi.fn(), post } as unknown as EshuApiClient;

    renderDeadCode(<DeadCodePage client={client} model={{ ...demoModel, findings: [] }} />, [
      "/dead-code?language=typescript",
    ]);

    await screen.findByText("Unreferenced symbol unusedRoute");
    fireEvent.click(screen.getByRole("button", { name: /trait/i }));
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        candidate_kind: "Trait",
        language: "typescript",
        limit: 100,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Show repository breakdown" }));
    const filteredLink = screen.getByRole("link", { name: "View candidates" });
    expect(filteredLink).toHaveAttribute(
      "href",
      "/dead-code?repo_id=repository%3Ar1&language=typescript&candidate_kind=Trait",
    );

    fireEvent.click(filteredLink);
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith("/api/v0/code/dead-code", {
        candidate_kind: "Trait",
        language: "typescript",
        limit: 100,
        repo_id: "repository:r1",
      }),
    );
  });

  it("does not invent a canonical repository scope for unresolved ownership", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          detail: "server/routes.ts · unused",
          entity: "unresolved repository",
          filePath: "server/routes.ts",
          id: "function:f1",
          language: "typescript",
          title: "Unreferenced symbol unusedRoute",
          truth: "derived",
          type: "Dead code",
        },
      ],
    };

    renderDeadCode(<DeadCodePage model={model} />);
    fireEvent.click(screen.getByRole("button", { name: "Show repository breakdown" }));

    expect(screen.getByText("Canonical identifier unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "View candidates" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "server/routes.ts" })).not.toBeInTheDocument();
    expect(screen.getByText("server/routes.ts")).toBeInTheDocument();
  });
});

function renderDeadCode(
  element: ReactElement,
  initialEntries?: string[],
): ReturnType<typeof render> {
  return render(<MemoryRouter initialEntries={initialEntries}>{element}</MemoryRouter>);
}

function LocationSearch(): React.JSX.Element {
  return <output data-testid="location-search">{useLocation().search}</output>;
}
