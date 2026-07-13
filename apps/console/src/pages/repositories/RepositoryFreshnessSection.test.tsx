import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RepositoryFreshnessSection } from "./RepositoryFreshnessSection";
import type { EshuApiClient } from "../../api/client";

afterEach(() => {
  vi.restoreAllMocks();
});

function freshnessWire(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    verdict: "current",
    observed_commit: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
    observed_at: new Date().toISOString(),
    generation: null,
    stages: { collected: true, reduced: true, projected: true, materialized: true },
    outstanding_by_stage: [],
    shared_enrichment: { pending: false, pending_domains: [] },
    unobserved_push: null,
    as_of: new Date().toISOString(),
    scoped: false,
    ...overrides,
  };
}

// sequencedClient answers the freshness GET with the next entry in
// `responses` on each call, repeating the last entry once exhausted --
// mirrors OperationsPage.test.tsx's opsClient convention. `paths` records
// every requested URL so tests can assert on the ?expected_commit= param.
function sequencedClient(responses: readonly Record<string, unknown>[]): EshuApiClient & {
  calls: number;
  paths: string[];
} {
  let call = 0;
  const paths: string[] = [];
  const client = {
    get calls() {
      return call;
    },
    paths,
    get: async (path: string) => {
      if (!path.includes("/freshness")) throw new Error(`unexpected get ${path}`);
      paths.push(path);
      const idx = Math.min(call, responses.length - 1);
      call += 1;
      return { data: responses[idx], error: null, truth: null };
    },
  };
  return client as unknown as EshuApiClient & { calls: number; paths: string[] };
}

// deferredClient never auto-resolves a GET; the test calls resolveNext() to
// settle the oldest pending request when it wants to observe the in-flight
// (checking) state before the response lands.
function deferredClient(): EshuApiClient & {
  paths: string[];
  resolveNext: (wire: Record<string, unknown>) => void;
} {
  const paths: string[] = [];
  const resolvers: Array<(value: { data: unknown; error: null; truth: null }) => void> = [];
  const client = {
    paths,
    get: (path: string) => {
      if (!path.includes("/freshness")) throw new Error(`unexpected get ${path}`);
      paths.push(path);
      return new Promise((resolve) => {
        resolvers.push(resolve);
      });
    },
    resolveNext: (wire: Record<string, unknown>) => {
      const resolve = resolvers.shift();
      if (!resolve) throw new Error("no pending request to resolve");
      resolve({ data: wire, error: null, truth: null });
    },
  };
  return client as unknown as EshuApiClient & {
    paths: string[];
    resolveNext: (wire: Record<string, unknown>) => void;
  };
}

describe("RepositoryFreshnessSection", () => {
  it("renders the stage checklist and outstanding work while building", async () => {
    const client = sequencedClient([
      freshnessWire({
        verdict: "building",
        stages: { collected: true, reduced: true, projected: false, materialized: false },
        outstanding_by_stage: [{ stage: "project", status: "running", count: 5 }],
      }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText(/Indexing a1b2c3d4e5/)).toBeInTheDocument();
    expect(screen.getByText("projecting — 5 items left")).toBeInTheDocument();
    expect(screen.getByText("Collected")).toBeInTheDocument();
    expect(screen.getByText("project · running: 5")).toBeInTheDocument();
  });

  it("renders the shared-enrichment note when own stages are done but shared work is pending", async () => {
    const client = sequencedClient([
      freshnessWire({
        verdict: "building",
        stages: { collected: true, reduced: true, projected: true, materialized: false },
        shared_enrichment: {
          pending: true,
          pending_domains: [{ domain: "package_registry", count: 2 }],
        },
      }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );

    expect(
      await screen.findByText(/Cross-repo enrichment still running: package_registry \(2\)/),
    ).toBeInTheDocument();
  });

  it("keeps polling while the verdict is building, then stops once it reaches current", async () => {
    const client = sequencedClient([
      freshnessWire({
        verdict: "building",
        stages: { collected: true, reduced: true, projected: false, materialized: false },
        outstanding_by_stage: [{ stage: "project", status: "running", count: 2 }],
      }),
      freshnessWire({ verdict: "current" }),
    ]);

    render(
      <RepositoryFreshnessSection client={client} repoId="repository:payments-api" pollMs={30} />,
    );

    expect(await screen.findByText(/Indexing a1b2c3d4e5/)).toBeInTheDocument();

    await waitFor(
      () => expect(screen.getByText(/Current through a1b2c3d4e5/)).toBeInTheDocument(),
      {
        timeout: 2000,
      },
    );

    // Verdict is now "current" -- a stable state -- so no further poll should
    // fire. Wait several poll intervals and confirm the call count holds.
    const callsAfterCurrent = client.calls;
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(client.calls).toBe(callsAfterCurrent);
  });

  it("does not poll again once the verdict reaches current on the very first fetch", async () => {
    const client = sequencedClient([freshnessWire({ verdict: "current" })]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:checkout-service"
        pollMs={30}
      />,
    );

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();
    const callsAfterFirst = client.calls;
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(client.calls).toBe(callsAfterFirst);
  });

  it("renders 'Build complete' with no fabricated push/sha wording when current has an empty observed_commit", async () => {
    const client = sequencedClient([
      freshnessWire({ verdict: "current", observed_commit: "", observed_at: null }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:snapshot-scope"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText("Build complete")).toBeInTheDocument();
    expect(screen.queryByText(/through/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/push/i)).not.toBeInTheDocument();
  });

  it("shows an explicit unavailable message and does not render stale content", async () => {
    const client = {
      get: async () => {
        throw new Error("offline");
      },
    } as unknown as EshuApiClient;

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:checkout-service"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText("Freshness unavailable from this source.")).toBeInTheDocument();
  });

  it("renders nothing when no client is connected", () => {
    const { container } = render(
      <RepositoryFreshnessSection repoId="repository:checkout-service" />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("submits an expected commit and drives the fetch with expectedCommit", async () => {
    const client = sequencedClient([
      freshnessWire({ verdict: "current" }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    const input = screen.getByLabelText("Expected commit");
    fireEvent.change(input, {
      target: { value: "  bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  " },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));

    await waitFor(() =>
      expect(client.paths.at(-1)).toContain(
        "expected_commit=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      ),
    );
  });

  it("renders the 'expected commit not indexed yet' verdict once the fetch resolves as behind", async () => {
    const client = sequencedClient([
      freshnessWire({ verdict: "current" }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Expected commit"), {
      target: { value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));

    expect(await screen.findByText("Behind your commit")).toBeInTheDocument();
    expect(
      await screen.findByText(/eshu has aaaaaaaaaa; expected bbbbbbbbbb not indexed yet\./),
    ).toBeInTheDocument();
  });

  it("clears the expected commit input and reverts to the plain fetch", async () => {
    const client = sequencedClient([
      freshnessWire({ verdict: "current" }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
      freshnessWire({ verdict: "current" }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    const input = screen.getByLabelText("Expected commit");
    fireEvent.change(input, {
      target: { value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));
    expect(await screen.findByText("Behind your commit")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /clear/i }));

    await waitFor(() => expect(client.paths.at(-1)).not.toContain("expected_commit"));
    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();
    // The panel re-renders through a "Loading freshness…" state on refetch, so
    // the pre-clear `input` reference is a detached node -- re-query it.
    expect(screen.getByLabelText("Expected commit")).toHaveValue("");
  });

  it("clears the expected commit state on a repoId change so a SHA never leaks across repos", async () => {
    // RepoSourcePage keeps this section mounted across in-app navigation
    // between repos (repoId comes from a route param, not a remount), so a
    // SHA typed for one repo must never silently drive the fetch for another.
    const client = sequencedClient([
      freshnessWire({ verdict: "current" }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
      freshnessWire({ verdict: "current" }),
    ]);

    const { rerender } = render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );
    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Expected commit"), {
      target: { value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));
    expect(await screen.findByText("Behind your commit")).toBeInTheDocument();

    rerender(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:other-service"
        pollMs={50000}
      />,
    );

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();
    expect(screen.getByLabelText("Expected commit")).toHaveValue("");

    const otherServicePaths = client.paths.filter((path) => path.includes("other-service"));
    expect(otherServicePaths.length).toBeGreaterThan(0);
    expect(otherServicePaths.every((path) => !path.includes("expected_commit"))).toBe(true);
  });

  it("refetches on every explicit Check click, even re-submitting the same SHA", async () => {
    // Re-checking the same SHA (e.g. after pushing again with the same
    // commit, or just confirming the answer hasn't changed) is the feature's
    // primary operator loop -- React's state-bail-out on an unchanged
    // appliedExpectedCommit value must not silently swallow the second click.
    const client = sequencedClient([
      freshnessWire({ verdict: "current" }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
    ]);

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );
    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Expected commit"), {
      target: { value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));
    await waitFor(() => expect(client.calls).toBe(2));
    expect(await screen.findByText("Behind your commit")).toBeInTheDocument();

    // Same SHA, still in the field -- click Check again.
    fireEvent.click(screen.getByRole("button", { name: /check/i }));
    await waitFor(() => expect(client.calls).toBe(3));

    const shaPaths = client.paths.filter((path) =>
      path.includes("expected_commit=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
    );
    expect(shaPaths.length).toBe(2);
  });

  it("keeps the panel and input mounted, preserving focus, while a same-scope refetch is in flight", async () => {
    const client = deferredClient();

    render(
      <RepositoryFreshnessSection
        client={client}
        repoId="repository:payments-api"
        pollMs={50000}
      />,
    );
    client.resolveNext(freshnessWire({ verdict: "current" }));
    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();

    const input = screen.getByLabelText("Expected commit");
    input.focus();
    fireEvent.change(input, {
      target: { value: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
    });
    fireEvent.click(screen.getByRole("button", { name: /check/i }));

    // The refetch triggered by Check is still pending (deferredClient never
    // auto-resolves): the prior freshness content and the same focused input
    // element must still be in the document -- no blank "Loading freshness…"
    // state that would unmount the form and drop keyboard focus.
    expect(screen.getByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();
    expect(screen.getByLabelText("Expected commit")).toBe(input);
    expect(document.activeElement).toBe(input);

    client.resolveNext(
      freshnessWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
    );
    expect(await screen.findByText("Behind your commit")).toBeInTheDocument();
  });
});
