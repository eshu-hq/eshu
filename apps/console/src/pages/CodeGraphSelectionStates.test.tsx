import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import { codeGraphSelectionKey } from "./CodeGraphPageSupport";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";

const repositories: readonly RepoListItem[] = [
  repository("repository:r1", "service-one"),
  repository("repository:r2", "service-two"),
];

describe("CodeGraphPage repository state isolation", () => {
  it("keeps owner keys distinct when repository and entity identifiers contain colons", () => {
    expect(codeGraphSelectionKey("repository:a", "content-entity:b")).not.toBe(
      codeGraphSelectionKey("repository", "a:content-entity:b"),
    );
  });

  it("ignores a stale inventory response after switching repositories", async () => {
    const pending = new Map<string, Deferred<unknown>>();
    const client = clientWithInventory((repoId) => {
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
      pending.get("repository:r2")?.resolve(inventory("repository:r2", "betaSymbol")),
    );
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol");

    await act(async () =>
      pending.get("repository:r1")?.resolve(inventory("repository:r1", "alphaSymbol")),
    );
    expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol");
    expect(screen.queryByText("alphaSymbol")).not.toBeInTheDocument();
  });

  it("keeps the new repository isolated through error and retry", async () => {
    let repoTwoAttempts = 0;
    const client = clientWithInventory((repoId) => {
      if (repoId === "repository:r1") return Promise.resolve(inventory(repoId, "alphaSymbol"));
      repoTwoAttempts += 1;
      if (repoTwoAttempts === 1)
        return Promise.reject(new Error("repository two inventory failed"));
      return Promise.resolve(inventory(repoId, "betaSymbol"));
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
    const client = clientWithInventory(
      (repoId) => Promise.resolve(inventory(repoId, "alphaSymbol")),
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
    const client = clientWithInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(inventory(repoId, "unexpectedSymbol"));
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
    const client = clientWithInventory(
      (repoId) => Promise.resolve(inventory(repoId, "alphaSymbol")),
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
    const client = clientWithInventory(() =>
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
    const client = clientWithInventory((repoId) => {
      inventoryCalls.push(repoId);
      return Promise.resolve(inventory(repoId, "unexpectedSymbol"));
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

  it("restores repository scope through browser back and forward", async () => {
    const client = clientWithInventory((repoId) =>
      Promise.resolve(inventory(repoId, repoId === "repository:r1" ? "alphaSymbol" : "betaSymbol")),
    );
    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
        <HistoryControls />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("alphaSymbol"),
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r2" },
    });
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1"),
    );
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent(
      "alphaSymbol",
    );
    fireEvent.click(screen.getByRole("button", { name: "Forward" }));
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r2"),
    );
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

function clientWithInventory(
  loadInventory: (repoId: string) => Promise<unknown>,
  rejectedEntityId = "",
  rejectionLimit = Number.POSITIVE_INFINITY,
): EshuApiClient {
  let storyRejections = 0;
  return {
    post: async (path: string, body: unknown) => {
      if (path === "/api/v0/code/structure/inventory") {
        return loadInventory(String((body as { readonly repo_id?: string }).repo_id));
      }
      if (path === "/api/v0/code/relationships/story") {
        const entityId = String((body as { readonly entity_id?: string }).entity_id);
        const repoId = String((body as { readonly repo_id?: string }).repo_id);
        if (entityId === rejectedEntityId && storyRejections < rejectionLimit) {
          storyRejections += 1;
          return {
            data: { relationships: [], target_resolution: { status: "not_found" } },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            entity_id: entityId,
            labels: ["Function"],
            name: entityId,
            relationships: [],
            scope: { repo_id: repoId },
            target_resolution: { entity_id: entityId, repo_id: repoId, status: "resolved" },
          },
          error: null,
          truth: null,
        };
      }
      if (path === "/api/v0/code/imports/investigate") {
        return { data: { cycles: [], truncated: false }, error: null, truth: null };
      }
      throw new Error(`unexpected request: ${path}`);
    },
  } as unknown as EshuApiClient;
}

function inventory(repoId: string, name: string): unknown {
  return {
    data: {
      results: [
        {
          entity_id: `content-entity:${repoId}:${name}`,
          entity_name: name,
          entity_type: "Function",
          file_path: `src/${name}.ts`,
          repo_id: repoId,
        },
      ],
      truncated: false,
    },
    error: null,
    truth: null,
  };
}

function repository(id: string, name: string): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: `platform/${name}`,
  };
}

function HistoryControls(): React.JSX.Element {
  const navigate = useNavigate();
  return (
    <>
      <button type="button" onClick={() => navigate(-1)}>
        Back
      </button>
      <button type="button" onClick={() => navigate(1)}>
        Forward
      </button>
    </>
  );
}

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly reject: (reason?: unknown) => void;
  readonly resolve: (value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, reject, resolve };
}
