import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { CodeGraphPage } from "./CodeGraphPage";
import { codeGraphSelectionKey } from "./CodeGraphPageSupport";
import {
  clientWithCodeGraphInventory,
  codeGraphInventory,
  codeGraphRepository,
  deferred,
  type Deferred,
} from "./codeGraphTestFixtures";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";

const repositories: readonly RepoListItem[] = [
  codeGraphRepository("repository:r1", "service-one"),
  codeGraphRepository("repository:r2", "service-two"),
];

describe("CodeGraphPage repository state isolation", () => {
  it("keeps owner keys distinct when repository and entity identifiers contain colons", () => {
    expect(codeGraphSelectionKey("repository:a", "content-entity:b")).not.toBe(
      codeGraphSelectionKey("repository", "a:content-entity:b"),
    );
  });

  it("ignores a stale inventory response after switching repositories", async () => {
    const pending = new Map<string, Deferred<unknown>>();
    const client = clientWithCodeGraphInventory((repoId) => {
      const request = deferred<unknown>();
      pending.set(repoId, request);
      return request.promise;
    });

    renderPage(client);
    await waitFor(() => expect(pending.has("repository:r1")).toBe(true));
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r2" },
    });

    expect(screen.getByText("Loading repository symbols.")).toBeInTheDocument();
    expect(screen.queryByText("alphaSymbol")).not.toBeInTheDocument();
    await waitFor(() => expect(pending.has("repository:r2")).toBe(true));
    await act(async () =>
      pending.get("repository:r2")?.resolve(codeGraphInventory("repository:r2", "betaSymbol")),
    );
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol");

    await act(async () =>
      pending.get("repository:r1")?.resolve(codeGraphInventory("repository:r1", "alphaSymbol")),
    );
    expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol");
    expect(screen.queryByText("alphaSymbol")).not.toBeInTheDocument();
  });

  it("keeps the new repository isolated through error and retry", async () => {
    let repoTwoAttempts = 0;
    const client = clientWithCodeGraphInventory((repoId) => {
      if (repoId === "repository:r1")
        return Promise.resolve(codeGraphInventory(repoId, "alphaSymbol"));
      repoTwoAttempts += 1;
      if (repoTwoAttempts === 1)
        return Promise.reject(new Error("repository two inventory failed"));
      return Promise.resolve(codeGraphInventory(repoId, "betaSymbol"));
    });

    renderPage(client);
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("alphaSymbol"),
    );
    await waitFor(() =>
      expect(document.querySelector(".gcanvas-svg")).toHaveTextContent("alphaSymbol"),
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r2" },
    });

    expect(document.querySelector(".gcanvas-svg")).toBeNull();
    expect(await screen.findByText("repository two inventory failed")).toBeInTheDocument();
    expect(screen.queryByText("alphaSymbol")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry repository graph" }));
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol"),
    );
    expect(screen.queryByText("alphaSymbol")).not.toBeInTheDocument();
  });

  it("retries a relationship-story error without changing repository scope", async () => {
    const entityId = "content-entity:repository:r1:alphaSymbol";
    const client = clientWithCodeGraphInventory(
      (repoId) => Promise.resolve(codeGraphInventory(repoId, "alphaSymbol")),
      entityId,
      1,
    );

    renderPage(client);
    expect(
      await screen.findByText("code relationship target not_found in the selected repository"),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1");
    fireEvent.click(screen.getByRole("button", { name: "Retry relationship graph" }));

    await waitFor(() => expect(document.querySelector(".gcanvas-svg")).toHaveTextContent(entityId));
    expect(
      screen.queryByText("code relationship target not_found in the selected repository"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1");
  });

  it("does not substitute a repository for an invalid deep link", async () => {
    const inventoryCalls: string[] = [];
    const client = clientWithCodeGraphInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(codeGraphInventory(repoId, "unexpectedSymbol"));
    });

    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Amissing"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("");
    expect(screen.getByText(/is not present in this session catalog/)).toBeInTheDocument();
    expect(inventoryCalls).toEqual([]);
  });

  it("does not substitute a symbol for an invalid entity deep link", async () => {
    const client = clientWithCodeGraphInventory(
      (repoId) => Promise.resolve(codeGraphInventory(repoId, "alphaSymbol")),
      "content-entity:missing",
    );
    render(
      <MemoryRouter
        initialEntries={["/code-graph?repo_id=repository%3Ar1&entity_id=content-entity%3Amissing"]}
      >
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(
      await screen.findByText("code relationship target not_found in the selected repository"),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1");
    expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveValue("content-entity:missing");
    expect(document.querySelector(".gcanvas-svg")).toBeNull();
  });

  it("loads an explicit authorized entity anchor beyond the bounded inventory page", async () => {
    const client = clientWithCodeGraphInventory(() =>
      Promise.resolve({ data: { results: [] }, error: null, truth: null }),
    );
    render(
      <MemoryRouter
        initialEntries={["/code-graph?repo_id=repository%3Ar1&entity_id=content-entity%3Aexplicit"]}
      >
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveValue(
      "content-entity:explicit",
    );
    await waitFor(() =>
      expect(document.querySelector(".gcanvas-svg")).toHaveTextContent("content-entity:explicit"),
    );
  });

  it("does not restart a pending relationship read for an explicit entity anchor", async () => {
    let storyCalls = 0;
    const pendingStory = new Promise<never>(() => undefined);
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/structure/inventory") {
          return { data: { results: [] }, error: null, truth: null };
        }
        if (path === "/api/v0/code/relationships/story") {
          storyCalls += 1;
          return pendingStory;
        }
        if (path === "/api/v0/code/imports/investigate") {
          return { data: { cycles: [], truncated: false }, error: null, truth: null };
        }
        throw new Error(`unexpected request: ${path}`);
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={["/code-graph?repo_id=repository%3Ar1&entity_id=content-entity%3Aexplicit"]}
      >
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    await waitFor(() => expect(storyCalls).toBeGreaterThan(0));
    await act(async () => {
      await Promise.resolve();
    });
    expect(storyCalls).toBe(1);
  });

  it("does not substitute the first repository for an invalid legacy candidate", () => {
    const inventoryCalls: string[] = [];
    const client = clientWithCodeGraphInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(codeGraphInventory(repoId, "unexpectedSymbol"));
    });
    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=missing-candidate"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText(/Legacy Code Graph candidate/)).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("");
    expect(inventoryCalls).toEqual([]);
  });

  it("resolves a legacy candidate repository label to its canonical catalog repository", async () => {
    const inventoryCalls: string[] = [];
    const client = clientWithCodeGraphInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(codeGraphInventory(repoId, "betaSymbol"));
    });
    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=legacy-beta"]}>
        <CodeGraphPage
          client={client}
          model={{
            ...demoModel,
            findings: [
              {
                detail: "src/beta.ts · unused",
                entity: "service-two",
                entityId: "content-entity:repository:r2:betaSymbol",
                filePath: "src/beta.ts",
                id: "legacy-beta",
                title: "Unreferenced symbol betaSymbol",
                truth: "derived",
                type: "Dead code",
              },
            ],
            source: "live",
          }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    await waitFor(() => expect(inventoryCalls).toEqual(["repository:r2"]));
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r2");
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveValue(
      "content-entity:repository:r2:betaSymbol",
    );
  });

  it("does not substitute the first repository when a legacy repository label is unavailable", () => {
    const inventoryCalls: string[] = [];
    const client = clientWithCodeGraphInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(codeGraphInventory(repoId, "unexpectedSymbol"));
    });
    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=legacy-unknown"]}>
        <CodeGraphPage
          client={client}
          model={{
            ...demoModel,
            findings: [
              {
                detail: "src/unknown.ts · unused",
                entity: "unknown-service",
                id: "legacy-unknown",
                title: "Unreferenced symbol unknownSymbol",
                truth: "derived",
                type: "Dead code",
              },
            ],
            source: "live",
          }}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    expect(
      screen.getByText(/repository is not present in this session catalog/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("");
    expect(inventoryCalls).toEqual([]);
  });
});

function renderPage(client: EshuApiClient): void {
  render(
    <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
      <CodeGraphPage
        client={client}
        model={{ ...demoModel, findings: [], source: "live" }}
        repositories={repositories}
      />
    </MemoryRouter>,
  );
}
