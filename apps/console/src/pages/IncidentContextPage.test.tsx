import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { vi } from "vitest";

import { IncidentContextPage } from "./IncidentContextPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("IncidentContextPage", () => {
  it("renders a deep-linked incident context packet with truth and evidence slots", async () => {
    const onOpenService = vi.fn();
    const client = {
      get: async (path: string) => {
        expect(path).toBe(
          "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&service_id=P-SVC&limit=25"
        );
        return {
          data: incidentContextPayload(),
          error: null,
          truth: truthEnvelope()
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/incidents?incident_id=PABC123&provider=pagerduty&scope_id=pd-prod&service_id=P-SVC"]}>
        <IncidentContextPage
          client={client}
          model={modelFromSnapshot(emptySnapshot("live"))}
          onOpenService={onOpenService}
        />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Incident context" })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getAllByText("Checkout elevated error rate").length).toBeGreaterThan(0);
    });
    expect(screen.getByText("incident.context.read")).toBeInTheDocument();
    expect(screen.getAllByText("derived").length).toBeGreaterThan(0);
    expect(screen.getByText("fresh")).toBeInTheDocument();
    expect(screen.getAllByText("PABC123").length).toBeGreaterThan(0);
    expect(screen.getByText("triggered")).toBeInTheDocument();
    expect(screen.getByText("intended routing")).toBeInTheDocument();
    expect(screen.getByText("Terraform declares checkout-api as the intended PagerDuty service.")).toBeInTheDocument();
    expect(screen.getAllByText("work item").length).toBeGreaterThan(0);
    expect(screen.getByText("no Jira link")).toBeInTheDocument();
    expect(screen.getByText("checkout-stage")).toBeInTheDocument();
    expect(screen.getByText("checkout-api deploy")).toBeInTheDocument();
    expect(screen.getByText("Incident triggered")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Open service" }));
    expect(onOpenService).toHaveBeenCalledWith("checkout-api");
    expect(screen.getByRole("link", { name: "Impact" })).toHaveAttribute(
      "href",
      "/impact?kind=service&target=checkout-api"
    );
  });

  it("submits the form into a deep-linkable incident context URL", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: incidentContextPayload(),
          error: null,
          truth: truthEnvelope()
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/incidents"]}>
        <Routes>
          <Route
            element={(
              <>
                <IncidentContextPage
                  client={client}
                  model={modelFromSnapshot(emptySnapshot("live"))}
                  onOpenService={vi.fn()}
                />
                <LocationProbe />
              </>
            )}
            path="/incidents"
          />
          <Route
            element={(
              <>
                <IncidentContextPage
                  client={client}
                  model={modelFromSnapshot(emptySnapshot("live"))}
                  onOpenService={vi.fn()}
                />
                <LocationProbe />
              </>
            )}
            path="/incidents/:incidentId/context"
          />
        </Routes>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Incident id"), { target: { value: "PABC123" } });
    fireEvent.change(screen.getByLabelText("Scope id"), { target: { value: "pd-prod" } });
    fireEvent.click(screen.getByRole("button", { name: "Review incident" }));

    await waitFor(() => {
      expect(calls).toEqual([
        "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&limit=25"
      ]);
    });
    expect(screen.getByTestId("incident-context-location")).toHaveTextContent(
      "/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod"
    );
    expect(screen.getAllByText("Checkout elevated error rate").length).toBeGreaterThan(0);
  });

  it("loads a canonical incident context route with query filters", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: incidentContextPayload(),
          error: null,
          truth: truthEnvelope()
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&service_id=P-SVC"]}>
        <Routes>
          <Route
            element={(
              <IncidentContextPage
                client={client}
                model={modelFromSnapshot(emptySnapshot("live"))}
                onOpenService={vi.fn()}
              />
            )}
            path="/incidents/:incidentId/context"
          />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(calls).toEqual([
        "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&service_id=P-SVC&limit=25"
      ]);
    });
  });

  it("shows unavailable incident envelopes without rendering stale context", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "ambiguous",
          message: "incident id matched multiple scopes"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/incidents?incidentId=PABC123"]}>
        <IncidentContextPage
          client={client}
          model={modelFromSnapshot(emptySnapshot("live"))}
          onOpenService={vi.fn()}
        />
      </MemoryRouter>
    );

    expect(await screen.findByText("ambiguous: incident id matched multiple scopes")).toBeInTheDocument();
    expect(screen.queryByText("Checkout elevated error rate")).not.toBeInTheDocument();
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="incident-context-location">{location.pathname + location.search}</output>;
}

function truthEnvelope() {
  return {
    basis: "active_incident_facts",
    capability: "incident.context.read",
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative"
  };
}

function incidentContextPayload(): Record<string, unknown> {
  return {
    ambiguous_evidence: [{
      candidates: [{ id: "P-SVC-ALT", label: "checkout-stage", reason: "same incident id in staging" }],
      explanation: "scope_id required to choose one PagerDuty scope",
      slot: "service",
      truth_label: "ambiguous"
    }],
    answer_metadata: {
      coverage: { limit: 25, query_shape: "incident_context_evidence_path" },
      missing_evidence: [{ reason: "no Jira link", slot: "work_item" }],
      partial_reasons: ["pull_request_missing"],
      recommended_next_calls: [{ tool: "list_work_item_evidence" }],
      truncated: false
    },
    evidence_path: [{
      evidence: [{ fact_id: "incident-fact", kind: "incident.record", source: "pagerduty" }],
      explanation: "PagerDuty incident record selected by provider incident id.",
      slot: "incident",
      truth_label: "exact",
      value: { provider_incident_id: "PABC123", title: "Checkout elevated error rate" }
    }, {
      evidence: [{ fact_id: "tf-routing", kind: "PagerDutyDeclaration", source: "terraform" }],
      explanation: "Terraform declares checkout-api as the intended PagerDuty service.",
      slot: "intended_routing",
      truth_label: "derived",
      value: { service_name: "checkout-api" }
    }, {
      explanation: "No Jira work item linked to the provider incident.",
      slot: "work_item",
      truth_label: "missing"
    }],
    incident: {
      created_at: "2026-06-13T15:04:05Z",
      incident_number: 42,
      priority: { summary: "P1" },
      provider: "pagerduty",
      provider_incident_id: "PABC123",
      scope_id: "pd-prod",
      service: { id: "P-SVC", summary: "checkout-api", type: "pagerduty_service" },
      status: "triggered",
      title: "Checkout elevated error rate",
      urgency: "high"
    },
    missing_evidence: [{ reason: "no Jira link", slot: "work_item" }],
    query: {
      limit: 25,
      provider: "pagerduty",
      provider_incident_id: "PABC123",
      scope_id: "pd-prod"
    },
    related_changes: [{
      change_id: "deploy-123",
      explanation: "fallback service/time match only",
      services: [{ id: "P-SVC", summary: "checkout-api" }],
      source: "github_actions",
      summary: "checkout-api deploy",
      timestamp: "2026-06-13T15:00:00Z",
      truth_label: "fallback"
    }],
    timeline: [{
      created_at: "2026-06-13T15:05:00Z",
      event_id: "evt-1",
      event_type: "trigger",
      summary: "Incident triggered"
    }],
    truncated: false
  };
}
