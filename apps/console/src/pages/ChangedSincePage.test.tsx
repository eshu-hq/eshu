import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { EshuApiClient } from "../api/client";
import { ChangedSincePage } from "./ChangedSincePage";

function envelope(data: unknown, capability = "freshness.changed_since", freshness = "fresh") {
  return {
    data,
    error: null,
    truth: {
      basis: "semantic_facts",
      capability,
      freshness: { state: freshness },
      level: "exact",
      profile: "production"
    }
  };
}

function fakeClient(calls: string[]): EshuApiClient {
  return {
    get: vi.fn(async (path: string) => {
      calls.push(path);
      if (path.startsWith("/api/v0/freshness/generations")) {
        return envelope({
          count: 2,
          generations: [{
            collector_kind: "git",
            current_active_generation_id: "gen-current",
            generation_id: "gen-prior",
            is_active: false,
            observed_at: "2026-06-12T18:00:00Z",
            queue_status: { dead_letter: 0, failed: 0, in_flight: 0, outstanding: 0, retrying: 0, succeeded: 9, total: 9 },
            scope_id: "git-repository-scope:acme/app",
            scope_kind: "repository",
            source_system: "github",
            status: "superseded",
            trigger_kind: "scheduled"
          }, {
            collector_kind: "git",
            current_active_generation_id: "gen-current",
            generation_id: "gen-current",
            is_active: true,
            observed_at: "2026-06-13T18:00:00Z",
            queue_status: { dead_letter: 0, failed: 0, in_flight: 0, outstanding: 0, retrying: 0, succeeded: 11, total: 11 },
            scope_id: "git-repository-scope:acme/app",
            scope_kind: "repository",
            source_system: "github",
            status: "active",
            trigger_kind: "scheduled"
          }],
          limit: 50,
          truncated: false
        }, "freshness.generation_lifecycle");
      }
      if (path.startsWith("/api/v0/freshness/services/changed-since")) {
        return envelope({
          categories: [{
            category: "ownership",
            counts: { added: 1, updated: 0, unchanged: 1, retired: 0, superseded: 0 },
            samples: { added: [{ fact_kind: "service_owner", stable_fact_key: "team/platform" }] },
            unavailable: false
          }],
          current_active_generation_id: "svc-gen-current",
          sample_limit: 25,
          service_id: "svc-checkout",
          since_generation_id: "svc-gen-prior",
          unavailable: false
        }, "freshness.service_changed_since");
      }
      if (path.startsWith("/api/v0/freshness/changed-since") && path.includes("gen-pruned")) {
        return envelope({
          categories: [],
          current_active_generation_id: "",
          repository: "acme/app",
          sample_limit: 25,
          scope_id: "git-repository-scope:acme/app",
          scope_kind: "repository",
          since_generation_id: "gen-pruned",
          unavailable: true,
          unavailable_reason: "retention_expired"
        }, "freshness.changed_since", "unavailable");
      }
      return envelope({
        categories: [{
          category: "files",
          counts: { added: 2, updated: 1, unchanged: 5, retired: 1, superseded: 0 },
          samples: {
            added: [{ fact_kind: "file", stable_fact_key: "src/main.go" }],
            retired: [{ fact_kind: "file", stable_fact_key: "legacy/config.yaml" }]
          },
          truncated: { added: false, retired: false },
          unavailable: false
        }, {
          category: "facts",
          counts: { added: 0, updated: 2, unchanged: 8, retired: 0, superseded: 1 },
          samples: {
            updated: [{ fact_kind: "terraform_resource", stable_fact_key: "aws_lambda_function.checkout" }]
          },
          unavailable: false
        }],
        current_active_generation_id: "gen-current",
        current_observed_at: "2026-06-13T18:00:00Z",
        repository: "acme/app",
        sample_limit: 25,
        scope_id: "git-repository-scope:acme/app",
        scope_kind: "repository",
        since_generation_id: "gen-prior",
        unavailable: false
      });
    })
  } as unknown as EshuApiClient;
}

describe("ChangedSincePage", () => {
  it("renders repository deltas, generation lifecycle context, truth, and blast-radius links", async () => {
    const calls: string[] = [];
    render(
      <MemoryRouter initialEntries={["/changed-since?mode=repository&repository=acme/app&since_generation_id=gen-prior"]}>
        <ChangedSincePage client={fakeClient(calls)} />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Changed Since" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("src/main.go")).toBeInTheDocument());
    expect(screen.getByText("gen-prior -> gen-current")).toBeInTheDocument();
    expect(screen.getByText("files")).toBeInTheDocument();
    expect(screen.getByText("terraform_resource")).toBeInTheDocument();
    expect(screen.getAllByTitle("Truth: exact").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Open blast radius" })).toHaveAttribute(
      "href",
      "/impact?kind=repository&target=acme%2Fapp"
    );
    expect(calls.some((path) => path.startsWith("/api/v0/freshness/generations"))).toBe(true);
  });

  it("renders service changed-since deltas with service impact links", async () => {
    const calls: string[] = [];
    render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage client={fakeClient(calls)} />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "service" } });
    fireEvent.change(screen.getByLabelText("Service ID"), { target: { value: "svc-checkout" } });
    fireEvent.change(screen.getByLabelText("Since generation"), { target: { value: "svc-gen-prior" } });
    fireEvent.click(screen.getByRole("button", { name: "Load changes" }));

    await waitFor(() => expect(screen.getByText("team/platform")).toBeInTheDocument());
    expect(screen.getByText("ownership")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open service impact" })).toHaveAttribute(
      "href",
      "/impact?kind=service&target=svc-checkout"
    );
    expect(calls.some((path) => path.startsWith("/api/v0/freshness/services/changed-since"))).toBe(true);
  });

  it("shows retention-expired unavailable state without pretending no changes exist", async () => {
    render(
      <MemoryRouter initialEntries={["/changed-since?mode=repository&repository=acme/app&since_generation_id=gen-pruned"]}>
        <ChangedSincePage client={fakeClient([])} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("retention_expired")).toBeInTheDocument());
    expect(screen.getByText("Changed-since data unavailable")).toBeInTheDocument();
  });

  it("does not call changed-since endpoints before a bounded selector and baseline exist", () => {
    const get = vi.fn();
    const client = { get } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <ChangedSincePage client={client} />
      </MemoryRouter>
    );

    expect(screen.getByText("Choose a repository/scope or service and a baseline to load changed-since evidence.")).toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
  });
});
