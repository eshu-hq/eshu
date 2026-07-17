import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ChangedSincePage } from "./ChangedSincePage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";

function repository(id: string, name: string): RepoListItem {
  return {
    groupKey: "",
    groupKind: "",
    groupReason: "",
    groupSource: "",
    groupTruth: "",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: `acme/${name}`,
  };
}

const repositories = [
  repository("repository:r_a", "app"),
  repository("repository:r_b", "payments"),
  repository("repository:r_c", "orders"),
] as const;

function envelope(data: unknown, capability = "freshness.changed_since") {
  return {
    data,
    error: null,
    truth: {
      basis: "semantic_facts",
      capability,
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production",
    },
  };
}

function lifecycle(repositorySuffix: string, includePrior = true) {
  const generations = includePrior
    ? [
        generation(repositorySuffix, "prior", false, "superseded"),
        generation(repositorySuffix, "current", true, "active"),
      ]
    : [generation(repositorySuffix, "current", true, "active")];
  return envelope(
    { count: generations.length, generations, limit: 3, truncated: false },
    "freshness.generation_lifecycle",
  );
}

function generation(
  repositorySuffix: string,
  generationSuffix: string,
  active: boolean,
  status: string,
) {
  return {
    current_active_generation_id: `gen-${repositorySuffix}-current`,
    generation_id: `gen-${repositorySuffix}-${generationSuffix}`,
    is_active: active,
    observed_at: active ? "2026-07-17T00:00:00Z" : "2026-07-16T00:00:00Z",
    queue_status: {},
    scope_id: `scope:${repositorySuffix}`,
    scope_kind: "repository",
    status,
  };
}

function changedPage(repositoryId: string, path: string) {
  return envelope({
    categories: [
      {
        category: "files",
        counts: { added: 1, retired: 0, superseded: 0, unchanged: 0, updated: 0 },
        samples: {
          added: [{ fact_kind: "file", stable_fact_key: `file:${repositoryId}:${path}` }],
        },
        unavailable: false,
      },
    ],
    current_active_generation_id: `gen-${repositoryId.slice(-1)}-current`,
    repository: repositoryId,
    sample_limit: 25,
    since_generation_id: `gen-${repositoryId.slice(-1)}-prior`,
    unavailable: false,
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("ChangedSincePage repository ownership", () => {
  it("restores the canonical repository selector for a copied legacy scope URL", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.startsWith("/api/v0/freshness/generations")) return lifecycle("b");
      return changedPage("repository:r_b", "src/payments.go");
    });

    renderPage(
      get,
      "/changed-since?mode=repository&scope_id=scope%3Ab&since_generation_id=gen-b-prior",
    );

    await screen.findAllByText("src/payments.go");
    expect(screen.getByLabelText<HTMLSelectElement>("Repository").value).toBe("repository:r_b");
  });

  it("searches the authorized catalog locally and keeps canonical IDs in each option", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.startsWith("/api/v0/freshness/generations")) return lifecycle("a");
      return changedPage("repository:r_a", "src/old.go");
    });

    renderPage(get);
    await screen.findAllByText("src/old.go");
    const requestsBeforeSearch = get.mock.calls.length;
    fireEvent.change(screen.getByLabelText("Search repositories"), {
      target: { value: "acme/payments" },
    });

    expect(screen.getByRole("option", { name: "payments · repository:r_b" })).toHaveValue(
      "repository:r_b",
    );
    expect(
      screen.queryByRole("option", { name: "orders · repository:r_c" }),
    ).not.toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(requestsBeforeSearch);
  });

  it("clears old evidence immediately and loads one canonical repository selector", async () => {
    const calls: string[] = [];
    const get = vi.fn(async (path: string) => {
      calls.push(path);
      if (path.startsWith("/api/v0/freshness/generations")) {
        return lifecycle(path.includes("r_b") ? "b" : "a");
      }
      return path.includes("r_b")
        ? changedPage("repository:r_b", "src/payments.go")
        : changedPage("repository:r_a", "src/old.go");
    });

    renderPage(get);
    await screen.findAllByText("src/old.go");
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "repository:r_b" } });

    expect(screen.queryAllByText("src/old.go")).toHaveLength(0);
    await waitFor(() =>
      expect(
        calls.some(
          (path) =>
            path.includes("repository=repository%3Ar_b") &&
            path.includes("since_generation_id=gen-b-prior"),
        ),
      ).toBe(true),
    );
    expect((await screen.findAllByText("src/payments.go")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("file:repository:r_b:src/payments.go").length).toBeGreaterThan(0);
    expect(calls.some((path) => path.includes("scope_id=") && path.includes("repository="))).toBe(
      false,
    );
  });

  it("shows a precise no-baseline state without issuing a changed-since read", async () => {
    const calls: string[] = [];
    const get = vi.fn(async (path: string) => {
      calls.push(path);
      if (path.startsWith("/api/v0/freshness/generations")) return lifecycle("b", false);
      return changedPage("repository:r_a", "src/old.go");
    });

    renderPage(get);
    await screen.findAllByText("src/old.go");
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "repository:r_b" } });

    expect(
      await screen.findByText("No retained prior generation is available for this repository."),
    ).toBeInTheDocument();
    expect(
      calls.some(
        (path) => path.startsWith("/api/v0/freshness/changed-since") && path.includes("r_b"),
      ),
    ).toBe(false);
  });

  it("prevents a slow obsolete repository baseline from rewriting a newer selection", async () => {
    const slowB = deferred<ReturnType<typeof lifecycle>>();
    const calls: string[] = [];
    const get = vi.fn(async (path: string) => {
      calls.push(path);
      if (path.startsWith("/api/v0/freshness/generations")) {
        if (path.includes("r_b")) return slowB.promise;
        return lifecycle(path.includes("r_c") ? "c" : "a");
      }
      if (path.includes("r_c")) return changedPage("repository:r_c", "src/orders.go");
      if (path.includes("r_b")) return changedPage("repository:r_b", "src/payments.go");
      return changedPage("repository:r_a", "src/old.go");
    });

    renderPage(get);
    await screen.findAllByText("src/old.go");
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "repository:r_b" } });
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLSelectElement>("Repository").value).toBe("repository:r_b"),
    );
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "repository:r_c" } });
    await screen.findAllByText("src/orders.go");

    await act(async () => {
      slowB.resolve(lifecycle("b"));
      await slowB.promise;
    });
    expect(screen.queryByText("src/payments.go")).not.toBeInTheDocument();
    expect(screen.getAllByText("src/orders.go").length).toBeGreaterThan(0);
    expect(
      calls.some(
        (path) => path.startsWith("/api/v0/freshness/changed-since") && path.includes("r_b"),
      ),
    ).toBe(false);
  });

  it("clears populated evidence and lifecycle while browser Back restores another repository", async () => {
    const slowLifecycleA = deferred<ReturnType<typeof lifecycle>>();
    const slowChangedA = deferred<ReturnType<typeof changedPage>>();
    let restoringA = false;
    const get = vi.fn(async (path: string) => {
      if (path.startsWith("/api/v0/freshness/generations")) {
        if (path.includes("r_a") && restoringA) return slowLifecycleA.promise;
        return lifecycle(path.includes("r_b") ? "b" : "a");
      }
      if (path.includes("r_a") && restoringA) return slowChangedA.promise;
      return path.includes("r_b")
        ? changedPage("repository:r_b", "src/payments.go")
        : changedPage("repository:r_a", "src/old.go");
    });

    renderPage(get, undefined, true);
    await screen.findAllByText("src/old.go");
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "repository:r_b" } });
    await screen.findAllByText("src/payments.go");

    restoringA = true;
    fireEvent.click(screen.getByRole("button", { name: "Browser back" }));

    expect(screen.queryByText("src/payments.go")).not.toBeInTheDocument();
    expect(screen.queryByText("gen-b-current")).not.toBeInTheDocument();

    await act(async () => {
      slowLifecycleA.resolve(lifecycle("a"));
      slowChangedA.resolve(changedPage("repository:r_a", "src/old.go"));
      await Promise.all([slowLifecycleA.promise, slowChangedA.promise]);
    });
    expect((await screen.findAllByText("src/old.go")).length).toBeGreaterThan(0);
  });
});

function renderPage(
  get: ReturnType<typeof vi.fn>,
  entry = "/changed-since?mode=repository&repository=repository%3Ar_a&since_generation_id=gen-a-prior",
  withHistoryControl = false,
): void {
  render(
    <MemoryRouter initialEntries={[entry]}>
      {withHistoryControl ? <HistoryBackButton /> : null}
      <ChangedSincePage client={{ get } as unknown as EshuApiClient} repositories={repositories} />
    </MemoryRouter>,
  );
}

function HistoryBackButton(): React.JSX.Element {
  const navigate = useNavigate();
  return <button onClick={() => navigate(-1)}>Browser back</button>;
}
