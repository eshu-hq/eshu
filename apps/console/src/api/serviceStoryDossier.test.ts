import { describe, expect, it } from "vitest";
import { serviceContextFromStoryDossier, type ServiceStoryDossierResponse } from "./serviceStoryDossier";

describe("serviceContextFromStoryDossier", () => {
  it("preserves target support evidence for repository workspace service stories", () => {
    const context = serviceContextFromStoryDossier(dossier, "api-node-boats");

    expect(context.support_overview?.target_support).toMatchObject({
      evidence_count: 1,
      incident_routing_count: 1,
      work_item_count: 0
    });
  });
});

const dossier: ServiceStoryDossierResponse = {
  service_identity: {
    repo_name: "api-node-boats",
    service_name: "api-node-boats"
  },
  support_overview: {
    target_support: {
      evidence: [
        {
          fact_id: "pd-service",
          fact_kind: "incident_routing.observed_pagerduty_service",
          payload: {
            outcome: "exact",
            provider: "pagerduty",
            service_id: "P-SVC",
            status: "active"
          }
        }
      ],
      evidence_count: 1,
      incident_routing_count: 1,
      work_item_count: 0
    }
  }
};
