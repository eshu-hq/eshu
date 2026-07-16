import { act, render, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ChangedSincePage } from "./ChangedSincePage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";

const repository: RepoListItem = {
  groupKey: "",
  groupKind: "",
  groupReason: "",
  groupSource: "",
  groupTruth: "",
  id: "acme/app",
  isDependency: false,
  name: "app",
  remoteUrl: "",
  repoSlug: "acme/app",
};

function pendingDiscoveryClient(requestSignals: AbortSignal[]): EshuApiClient {
  return {
    get: vi.fn(
      (_path: string, options?: { readonly signal?: AbortSignal }) =>
        new Promise<never>((_resolve, reject) => {
          const signal = options?.signal;
          if (!signal) throw new Error("lifecycle discovery requires an abort signal");
          requestSignals.push(signal);
          signal.addEventListener(
            "abort",
            () =>
              reject(
                signal.reason instanceof Error
                  ? signal.reason
                  : new DOMException("lifecycle discovery aborted", "AbortError"),
              ),
            { once: true },
          );
        }),
    ),
  } as unknown as EshuApiClient;
}

describe("ChangedSincePage lifecycle discovery ownership", () => {
  it("reuses one lifecycle discovery owner during StrictMode replay", async () => {
    const requestSignals: AbortSignal[] = [];
    const view = render(
      <StrictMode>
        <MemoryRouter initialEntries={["/changed-since"]}>
          <ChangedSincePage
            client={pendingDiscoveryClient(requestSignals)}
            repositories={[repository]}
          />
        </MemoryRouter>
      </StrictMode>,
    );

    await waitFor(() => expect(requestSignals).toHaveLength(1));
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(requestSignals[0]?.aborted).toBe(false);
    view.unmount();
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(requestSignals[0]?.aborted).toBe(true);
  });

  it("aborts stale lifecycle discovery when the repository scope changes", async () => {
    const requestSignals: AbortSignal[] = [];
    const client = pendingDiscoveryClient(requestSignals);
    const view = render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage client={client} repositories={[repository]} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(requestSignals).toHaveLength(1));
    view.rerender(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage
          client={client}
          repositories={[{ ...repository, id: "acme/other", repoSlug: "acme/other" }]}
        />
      </MemoryRouter>,
    );

    await waitFor(() => expect(requestSignals).toHaveLength(2));
    expect(requestSignals[0]?.aborted).toBe(true);
    expect(requestSignals[1]?.aborted).toBe(false);
  });

  it("keeps StrictMode discovery within five concurrent and 25 total probes", async () => {
    const repositories: readonly RepoListItem[] = Array.from({ length: 30 }, (_, index) => ({
      ...repository,
      id: `acme/repository-${index}`,
      repoSlug: `acme/repository-${index}`,
    }));
    let active = 0;
    let maximumActive = 0;
    let total = 0;
    const get = vi.fn(
      () =>
        new Promise((resolve) => {
          active += 1;
          total += 1;
          maximumActive = Math.max(maximumActive, active);
          setTimeout(() => {
            active -= 1;
            resolve({
              data: { count: 0, generations: [], limit: 2, truncated: false },
              error: null,
              truth: {
                basis: "semantic_facts",
                capability: "freshness.generation_lifecycle",
                freshness: { state: "fresh" },
                level: "exact",
                profile: "production",
              },
            });
          }, 0);
        }),
    );

    render(
      <StrictMode>
        <MemoryRouter initialEntries={["/changed-since"]}>
          <ChangedSincePage
            client={{ get } as unknown as EshuApiClient}
            repositories={repositories}
          />
        </MemoryRouter>
      </StrictMode>,
    );

    await waitFor(() => expect(total).toBe(25));
    expect(maximumActive).toBeLessThanOrEqual(5);
    expect(total).toBeLessThanOrEqual(25);
  });
});
