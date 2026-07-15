import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ChangedSincePage } from "./ChangedSincePage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";

const repositories: readonly RepoListItem[] = [
  {
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
  },
];

function ChangedSinceNavigationHarness({ client }: { readonly client: EshuApiClient }) {
  const navigate = useNavigate();
  return (
    <>
      <button
        type="button"
        onClick={() =>
          navigate(
            "/changed-since?mode=repository&repository=new-repo&since_generation_id=gen-new-prior",
          )
        }
      >
        Switch scope
      </button>
      <ChangedSincePage client={client} />
    </>
  );
}

function envelope(data: unknown, capability = "freshness.changed_since", freshness = "fresh") {
  return {
    data,
    error: null,
    truth: {
      basis: "semantic_facts",
      capability,
      freshness: { state: freshness },
      level: "exact",
      profile: "production",
    },
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function changedPage(repositoryName: string, factKey: string) {
  return envelope({
    categories: [
      {
        category: "files",
        counts: { added: 1, retired: 0, superseded: 0, unchanged: 0, updated: 0 },
        samples: { added: [{ fact_kind: "file", stable_fact_key: factKey }] },
        unavailable: false,
      },
    ],
    current_active_generation_id: `gen-${repositoryName}-current`,
    repository: repositoryName,
    sample_limit: 25,
    since_generation_id: `gen-${repositoryName}-prior`,
    unavailable: false,
  });
}

function fakeClient(calls: string[], discoverySignals: AbortSignal[] = []): EshuApiClient {
  return {
    get: vi.fn(async (path: string, options?: { readonly signal?: AbortSignal }) => {
      calls.push(path);
      if (path.startsWith("/api/v0/freshness/generations") && options?.signal) {
        discoverySignals.push(options.signal);
      }
      if (path.startsWith("/api/v0/freshness/generations")) {
        return envelope(
          {
            count: 2,
            generations: [
              {
                collector_kind: "git",
                current_active_generation_id: "gen-current",
                generation_id: "gen-prior",
                is_active: false,
                observed_at: "2026-06-12T18:00:00Z",
                queue_status: {
                  dead_letter: 0,
                  failed: 0,
                  in_flight: 0,
                  outstanding: 0,
                  retrying: 0,
                  succeeded: 9,
                  total: 9,
                },
                scope_id: "git-repository-scope:acme/app",
                scope_kind: "repository",
                source_system: "github",
                status: "superseded",
                trigger_kind: "scheduled",
              },
              {
                collector_kind: "git",
                current_active_generation_id: "gen-current",
                generation_id: "gen-current",
                is_active: true,
                observed_at: "2026-06-13T18:00:00Z",
                queue_status: {
                  dead_letter: 0,
                  failed: 0,
                  in_flight: 0,
                  outstanding: 0,
                  retrying: 0,
                  succeeded: 11,
                  total: 11,
                },
                scope_id: "git-repository-scope:acme/app",
                scope_kind: "repository",
                source_system: "github",
                status: "active",
                trigger_kind: "scheduled",
              },
            ],
            limit: 50,
            truncated: false,
          },
          "freshness.generation_lifecycle",
        );
      }
      if (path.startsWith("/api/v0/freshness/services/changed-since")) {
        return envelope(
          {
            categories: [
              {
                category: "ownership",
                counts: { added: 1, updated: 0, unchanged: 1, retired: 0, superseded: 0 },
                samples: {
                  added: [{ fact_kind: "service_owner", stable_fact_key: "team/platform" }],
                },
                unavailable: false,
              },
            ],
            current_active_generation_id: "svc-gen-current",
            sample_limit: 25,
            service_id: "svc-checkout",
            since_generation_id: "svc-gen-prior",
            unavailable: false,
          },
          "freshness.service_changed_since",
        );
      }
      if (path.startsWith("/api/v0/freshness/changed-since") && path.includes("gen-pruned")) {
        return envelope(
          {
            categories: [],
            current_active_generation_id: "",
            repository: "acme/app",
            sample_limit: 25,
            scope_id: "git-repository-scope:acme/app",
            scope_kind: "repository",
            since_generation_id: "gen-pruned",
            unavailable: true,
            unavailable_reason: "retention_expired",
          },
          "freshness.changed_since",
          "unavailable",
        );
      }
      return envelope({
        categories: [
          {
            category: "files",
            counts: { added: 2, updated: 1, unchanged: 5, retired: 1, superseded: 0 },
            samples: {
              added: [{ fact_kind: "file", stable_fact_key: "src/main.go" }],
              retired: [{ fact_kind: "file", stable_fact_key: "legacy/config.yaml" }],
              superseded: [{ fact_kind: "service_owner", stable_fact_key: "old/service-owner" }],
            },
            truncated: { added: false, retired: false, superseded: true },
            unavailable: false,
          },
          {
            category: "facts",
            counts: { added: 0, updated: 2, unchanged: 8, retired: 0, superseded: 1 },
            samples: {
              updated: [
                {
                  fact_kind: "terraform_resource",
                  stable_fact_key: "aws_lambda_function.checkout",
                },
              ],
            },
            unavailable: false,
          },
        ],
        current_active_generation_id: "gen-current",
        current_observed_at: "2026-06-13T18:00:00Z",
        repository: "acme/app",
        sample_limit: 25,
        scope_id: "git-repository-scope:acme/app",
        scope_kind: "repository",
        since_generation_id: "gen-prior",
        unavailable: false,
      });
    }),
  } as unknown as EshuApiClient;
}

describe("ChangedSincePage", () => {
  it("does not let an older scope overwrite newer changed-since or generation truth", async () => {
    const oldChanges = deferred<ReturnType<typeof envelope>>();
    const oldGenerations = deferred<ReturnType<typeof envelope>>();
    const get = vi.fn(async (path: string) => {
      const isOld = path.includes("repository=old-repo");
      if (path.startsWith("/api/v0/freshness/generations")) {
        if (isOld) return oldGenerations.promise;
        return envelope(
          {
            count: 1,
            generations: [
              {
                current_active_generation_id: "gen-new-current",
                generation_id: "gen-new-current",
                is_active: true,
                observed_at: "2026-07-14T00:00:00Z",
                queue_status: {},
                scope_id: "scope:new",
                status: "active",
              },
            ],
            limit: 50,
            truncated: false,
          },
          "freshness.generation_lifecycle",
        );
      }
      if (isOld) return oldChanges.promise;
      return changedPage("new-repo", "new/file.go");
    });
    render(
      <MemoryRouter
        initialEntries={[
          "/changed-since?mode=repository&repository=old-repo&since_generation_id=gen-old-prior",
        ]}
      >
        <ChangedSinceNavigationHarness client={{ get } as unknown as EshuApiClient} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByRole("button", { name: "Switch scope" }));

    expect((await screen.findAllByText("new/file.go")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("gen-new-current")).length).toBeGreaterThan(0);

    await act(async () => {
      oldChanges.resolve(changedPage("old-repo", "old/file.go"));
      oldGenerations.resolve(
        envelope(
          {
            count: 1,
            generations: [
              {
                current_active_generation_id: "gen-old-current",
                generation_id: "gen-old-current",
                is_active: true,
                observed_at: "2026-07-13T00:00:00Z",
                queue_status: {},
                scope_id: "scope:old",
                status: "active",
              },
            ],
            limit: 50,
            truncated: false,
          },
          "freshness.generation_lifecycle",
        ),
      );
      await Promise.all([oldChanges.promise, oldGenerations.promise]);
    });

    expect(screen.getAllByText("new/file.go").length).toBeGreaterThan(0);
    expect(screen.queryByText("old/file.go")).not.toBeInTheDocument();
    expect(screen.getAllByText("gen-new-current").length).toBeGreaterThan(0);
    expect(screen.queryByText("gen-old-current")).not.toBeInTheDocument();
  });

  it("renders repository deltas, generation lifecycle context, truth, and blast-radius links", async () => {
    const calls: string[] = [];
    render(
      <MemoryRouter
        initialEntries={[
          "/changed-since?mode=repository&repository=acme/app&since_generation_id=gen-prior",
        ]}
      >
        <ChangedSincePage client={fakeClient(calls)} repositories={repositories} />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "Changed Since" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText("src/main.go").length).toBeGreaterThan(0));
    expect(screen.getByText("gen-prior -> gen-current")).toBeInTheDocument();
    expect(screen.getByText("Evidence packet comparison")).toBeInTheDocument();
    expect(screen.getByText("Current generation")).toBeInTheDocument();
    expect(screen.getByText("Baseline generation")).toBeInTheDocument();
    expect(screen.getByText("25 samples per verdict; one bucket is truncated")).toBeInTheDocument();
    expect(screen.getAllByText("files").length).toBeGreaterThan(0);
    expect(screen.getAllByText("terraform_resource").length).toBeGreaterThan(0);
    expect(screen.getByText("removed/retracted")).toBeInTheDocument();
    expect(screen.getByText("stale/missing")).toBeInTheDocument();
    expect(screen.getAllByText("old/service-owner").length).toBeGreaterThan(0);
    expect(screen.getAllByTitle("Truth: exact").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Open blast radius" })).toHaveAttribute(
      "href",
      "/impact?kind=repository&target=acme%2Fapp",
    );
    expect(calls.some((path) => path.startsWith("/api/v0/freshness/generations"))).toBe(true);
  });

  it("renders service changed-since deltas with service impact links", async () => {
    const calls: string[] = [];
    render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage client={fakeClient(calls)} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "service" } });
    fireEvent.change(screen.getByLabelText("Service ID"), { target: { value: "svc-checkout" } });
    fireEvent.change(screen.getByLabelText("Since generation"), {
      target: { value: "svc-gen-prior" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Load changes" }));

    await waitFor(() => expect(screen.getAllByText("team/platform").length).toBeGreaterThan(0));
    expect(screen.getAllByText("ownership").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Open service impact" })).toHaveAttribute(
      "href",
      "/impact?kind=service&target=svc-checkout",
    );
    expect(calls.some((path) => path.startsWith("/api/v0/freshness/services/changed-since"))).toBe(
      true,
    );
  });

  it("auto-loads an exact active and prior repository generation pair on open", async () => {
    const calls: string[] = [];
    const discoverySignals: AbortSignal[] = [];
    const view = render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage
          client={fakeClient(calls, discoverySignals)}
          repositories={repositories}
        />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getAllByText("src/main.go").length).toBeGreaterThan(0));
    const changedSinceCall = calls.find((path) =>
      path.startsWith("/api/v0/freshness/changed-since"),
    );
    expect(changedSinceCall).toBeDefined();
    expect(changedSinceCall).toContain("scope_id=git-repository-scope%3Aacme%2Fapp");
    expect(changedSinceCall).toContain("since_generation_id=gen-prior");
    expect(screen.getByLabelText<HTMLInputElement>("Scope ID").value).toBe(
      "git-repository-scope:acme/app",
    );
    expect(screen.getByLabelText<HTMLInputElement>("Since generation").value).toBe("gen-prior");
    expect(calls[0]).toBe("/api/v0/freshness/generations?repository=acme%2Fapp&limit=2");
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(discoverySignals).toHaveLength(1);
    expect(discoverySignals[0]?.aborted).toBe(false);
    view.unmount();
    expect(discoverySignals[0]?.aborted).toBe(false);
  });

  it("shows retention-expired unavailable state without pretending no changes exist", async () => {
    render(
      <MemoryRouter
        initialEntries={[
          "/changed-since?mode=repository&repository=acme/app&since_generation_id=gen-pruned",
        ]}
      >
        <ChangedSincePage client={fakeClient([])} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("retention_expired")).toBeInTheDocument());
    expect(screen.getByText("Changed-since data unavailable")).toBeInTheDocument();
  });

  it("only performs bounded lifecycle discovery before a comparison pair exists", async () => {
    const get = vi.fn();
    const client = { get } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage client={client} repositories={repositories} />
      </MemoryRouter>,
    );

    expect(
      screen.getByText(
        "Choose a repository/scope or service and a baseline to load changed-since evidence.",
      ),
    ).toBeInTheDocument();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    const [path, options] = get.mock.calls[0] as unknown as [
      string,
      { readonly signal?: AbortSignal },
    ];
    expect(path).toBe("/api/v0/freshness/generations?repository=acme%2Fapp&limit=2");
    expect(options.signal).toBeInstanceOf(AbortSignal);
  });
});
