import { EshuApiClient } from "./client";
import { inspectionRequest } from "../test/inspectionRequest";
import { loadIncidentContext } from "./incidentContext";

describe("incident context adapter", () => {
  it("loads a bounded incident context packet with truth metadata", async () => {
    const calls: Request[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        calls.push(request);
        return Response.json({
          data: incidentContextPayload(),
          error: null,
          truth: truthEnvelope()
        });
      }
    });

    const result = await loadIncidentContext(client, {
      incidentId: "PABC123",
      limit: 250,
      provider: "pagerduty",
      scopeId: "pd-prod",
      serviceId: "P-SVC",
      since: "2026-06-13T15:00:00Z",
      until: "2026-06-13T16:00:00Z"
    });

    expect(result.status).toBe("ready");
    if (result.status !== "ready") {
      throw new Error("expected ready incident context");
    }
    const url = new URL(calls[0]?.url ?? "");
    expect(url.pathname).toBe("/api/v0/incidents/PABC123/context");
    expect(url.searchParams.get("provider")).toBe("pagerduty");
    expect(url.searchParams.get("scope_id")).toBe("pd-prod");
    expect(url.searchParams.get("service_id")).toBe("P-SVC");
    expect(url.searchParams.get("since")).toBe("2026-06-13T15:00:00Z");
    expect(url.searchParams.get("until")).toBe("2026-06-13T16:00:00Z");
    expect(url.searchParams.get("limit")).toBe("100");
    expect(result.context.incident.title).toBe("Checkout elevated error rate");
    expect(result.context.evidencePath.map((edge) => edge.slot)).toContain("intended_routing");
    expect(result.context.missingEvidence.map((missing) => missing.slot)).toContain("work_item");
    expect(result.context.truncated).toBe(true);
    expect(result.truth).not.toBeNull();
    if (result.truth === null) {
      throw new Error("expected incident context truth metadata");
    }
    expect(result.truth.capability).toBe("incident.context.read");
    expect(result.context.answerMetadata.recommendedNextCalls).toEqual([{
      args: { incident_id: "PABC123" },
      reason: "work item evidence is absent",
      route: "/api/v0/evidence/work-items",
      tool: "list_work_item_evidence"
    }]);
  });

  it("keeps envelope errors visible to the page", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (): Promise<Response> => Response.json({
        data: null,
        error: {
          code: "ambiguous",
          message: "incident id matched multiple scopes"
        },
        truth: null
      })
    });

    const result = await loadIncidentContext(client, {
      incidentId: "PABC123"
    });

    expect(result.status).toBe("unavailable");
    expect(result.status === "unavailable" ? result.error : "").toContain("ambiguous");
  });
});

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
      coverage: { limit: 100, query_shape: "incident_context_evidence_path" },
      missing_evidence: [{ reason: "no Jira link", slot: "work_item" }],
      partial_reasons: ["pull_request_missing"],
      recommended_next_calls: [{
        args: { incident_id: "PABC123" },
        reason: "work item evidence is absent",
        route: "/api/v0/evidence/work-items",
        tool: "list_work_item_evidence"
      }],
      truncated: true
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
      evidence_fact_id: "incident-fact",
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
      limit: 100,
      provider: "pagerduty",
      provider_incident_id: "PABC123",
      scope_id: "pd-prod",
      service_id: "P-SVC"
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
    truncated: true
  };
}
