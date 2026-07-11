// pages/GuidedQuestionsPage.test.tsx
// Verifies GuidedQuestionsPage (issue #4746):
//   - demo mode explains guided questions need a live connection and never
//     calls the API client (distinct from the demo_fixture provenance path)
//   - loading / empty / unavailable states for the live catalog fetch
//   - the catalog renders whatever playbooks the live API returns (generic,
//     not hardcoded)
//   - running a playbook validates required inputs, calls the resolver, and
//     renders the resolved bounded calls plus the truth envelope
//   - a resolver failure renders an inline, retryable error
//   - accessibility: labeled inputs, a named playbook list, and focus moving
//     to the results heading after a successful run
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { GuidedQuestionsPage } from "./GuidedQuestionsPage";
import type { EshuApiClient } from "../api/client";
import type { SourceState } from "../components/SourceControls";

function source(overrides: Partial<SourceState> = {}): SourceState {
  return {
    base: "https://eshu.example/api/",
    key: "shared",
    mode: "private",
    status: "connected",
    msg: "",
    ...overrides,
  };
}

const onePlaybookCatalog = {
  playbooks: [
    {
      id: "service_story_citation",
      name: "Service story with citation packet",
      version: "1.0.0",
      prompt_family: "service.story",
      description: "Answer a service story prompt and back it with evidence.",
      required_inputs: [
        {
          name: "service_name",
          type: "identifier",
          required: true,
          description: "Service to describe.",
        },
        {
          name: "environment",
          type: "string",
          required: false,
          description: "Optional environment.",
        },
      ],
      steps: [
        {
          id: "service_dossier",
          tool: "get_service_story",
          expected_truth: "deterministic",
          evidence_expected: "one-call service dossier",
        },
      ],
      failure_modes: [
        {
          condition: "service not found",
          meaning: "no matching service",
          fallback: "resolve_entity",
        },
      ],
    },
  ],
  versions: [{ id: "service_story_citation", version: "1.0.0" }],
  count: 1,
};

function clientReturningCatalog(catalog: unknown = onePlaybookCatalog): EshuApiClient {
  return {
    get: vi.fn(async () => ({
      data: catalog,
      error: null,
      truth: {
        capability: "query.playbooks",
        level: "exact",
        profile: "production",
        freshness: { state: "fresh" },
      },
    })),
    post: vi.fn(),
  } as unknown as EshuApiClient;
}

describe("GuidedQuestionsPage demo mode", () => {
  it("explains guided questions need a live connection and never fetches", () => {
    const client = clientReturningCatalog();
    render(<GuidedQuestionsPage client={client} source={source({ mode: "demo", key: "" })} />);

    expect(screen.getByText(/Guided questions need a live connection/i)).toBeInTheDocument();
    expect(client.get).not.toHaveBeenCalled();
  });
});

describe("GuidedQuestionsPage live catalog", () => {
  it("shows a loading state until the catalog resolves", () => {
    const client = { get: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<GuidedQuestionsPage client={client} source={source()} />);
    expect(screen.getByText(/Loading guided questions/i)).toBeInTheDocument();
  });

  it("shows an empty state when the live API returns no playbooks yet", async () => {
    const client = clientReturningCatalog({ playbooks: [], versions: [], count: 0 });
    render(<GuidedQuestionsPage client={client} source={source()} />);
    expect(await screen.findByText(/No guided questions are available/i)).toBeInTheDocument();
  });

  it("shows an unavailable state when the catalog request fails", async () => {
    const client = {
      get: async () => {
        throw new Error("HTTP 503");
      },
      post: vi.fn(),
    } as unknown as EshuApiClient;
    render(<GuidedQuestionsPage client={client} source={source()} />);
    expect(await screen.findByText(/Guided questions catalog unavailable/i)).toBeInTheDocument();
  });

  it("renders whatever playbooks the live API returns, not a hardcoded list", async () => {
    const client = clientReturningCatalog();
    render(<GuidedQuestionsPage client={client} source={source()} />);

    expect(await screen.findByText("Service story with citation packet")).toBeInTheDocument();
    expect(screen.getByText("service.story")).toBeInTheDocument();
    expect(screen.getByText(/Answer a service story prompt/)).toBeInTheDocument();
  });
});

describe("GuidedQuestionsPage running a playbook", () => {
  it("validates required inputs before calling the resolver", async () => {
    const client = clientReturningCatalog();
    render(<GuidedQuestionsPage client={client} source={source()} />);

    fireEvent.click(await screen.findByRole("button", { name: /Run/i }));
    fireEvent.click(screen.getByRole("button", { name: /Resolve/i }));

    expect(await screen.findByText(/service_name is required/i)).toBeInTheDocument();
    expect(client.post).not.toHaveBeenCalled();
  });

  it("resolves the playbook and renders the bounded calls with the truth envelope", async () => {
    const client = clientReturningCatalog();
    (client.post as ReturnType<typeof vi.fn>).mockResolvedValue({
      data: {
        resolved: {
          playbook_id: "service_story_citation",
          version: "1.0.0",
          prompt_family: "service.story",
          calls: [
            {
              step_id: "service_dossier",
              tool: "get_service_story",
              arguments: { service_name: "checkout-service" },
              expected_truth: "deterministic",
              evidence_expected: "one-call service dossier",
            },
          ],
          failure_modes: [
            {
              condition: "service not found",
              meaning: "no matching service",
              fallback: "resolve_entity",
            },
          ],
        },
      },
      error: null,
      truth: {
        capability: "query.playbooks",
        level: "exact",
        profile: "production",
        freshness: { state: "fresh" },
      },
    });

    render(<GuidedQuestionsPage client={client} source={source()} />);

    fireEvent.click(await screen.findByRole("button", { name: /Run/i }));
    fireEvent.change(screen.getByLabelText("service_name"), {
      target: { value: "checkout-service" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Resolve/i }));

    expect(await screen.findByText("get_service_story")).toBeInTheDocument();
    expect(client.post).toHaveBeenCalledWith("/api/v0/query-playbooks/resolve", {
      playbook_id: "service_story_citation",
      inputs: { service_name: "checkout-service" },
    });
    expect(screen.getByText("one-call service dossier")).toBeInTheDocument();
    expect(screen.getByText("deterministic")).toBeInTheDocument();
    expect(screen.getByText("query.playbooks")).toBeInTheDocument();
    expect(screen.getByText("service not found")).toBeInTheDocument();

    // Focus moves to the results heading so screen reader / keyboard users land
    // on the new content.
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Resolved plan" })).toHaveFocus(),
    );
  });

  it("renders a retryable inline error when the resolver fails", async () => {
    const client = clientReturningCatalog();
    (client.post as ReturnType<typeof vi.fn>).mockRejectedValue(
      Object.assign(new Error('required input "service_name" is missing'), {
        name: "EshuEnvelopeError",
      }),
    );

    render(<GuidedQuestionsPage client={client} source={source()} />);

    fireEvent.click(await screen.findByRole("button", { name: /Run/i }));
    fireEvent.change(screen.getByLabelText("service_name"), {
      target: { value: "checkout-service" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Resolve/i }));

    expect(await screen.findByText(/required input "service_name" is missing/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Try again/i })).toBeInTheDocument();
  });
});
