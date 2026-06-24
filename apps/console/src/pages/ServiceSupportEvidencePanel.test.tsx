import { fireEvent, render, screen, within } from "@testing-library/react";

import { ServiceSpotlightPanel } from "./ServiceSpotlightPanel";
import type { ServiceSpotlight } from "../api/serviceSpotlight";

describe("ServiceSupportEvidencePanel", () => {
  it("renders bounded Jira and PagerDuty support evidence without outbound links", () => {
    render(<ServiceSpotlightPanel spotlight={spotlightWithSupport} />);

    fireEvent.click(screen.getByRole("button", { name: "Impact review" }));

    const panel = screen.getByRole("region", { name: "Incidents and issues" });
    expect(within(panel).getByRole("heading", { name: "Incidents and issues" })).toBeInTheDocument();
    expect(within(panel).getByText("1 Jira/work item")).toBeInTheDocument();
    expect(within(panel).getByText("1 PagerDuty route")).toBeInTheDocument();
    expect(within(panel).getByText("1 ambiguous")).toBeInTheDocument();
    expect(within(panel).getByText("Jira PAY-123")).toBeInTheDocument();
    expect(within(panel).getByText("Incident")).toBeInTheDocument();
    expect(within(panel).getByText("In Progress")).toBeInTheDocument();
    expect(within(panel).getByText("https://jira.example.test/browse/PAY-123")).toBeInTheDocument();
    expect(within(panel).getByText("PagerDuty routing")).toBeInTheDocument();
    expect(within(panel).getByText("exact")).toBeInTheDocument();
    expect(within(panel).getByText("active")).toBeInTheDocument();
    expect(within(panel).queryAllByRole("link")).toHaveLength(0);
  });
});

const spotlightWithSupport: ServiceSpotlight = {
  api: { endpointCount: 0, endpoints: [], methodCount: 0, sourcePaths: [] },
  consumers: [],
  dependencies: [],
  deploymentGraph: { links: [], nodes: [] },
  graphDependents: [],
  hostnames: [],
  investigation: {
    coverage: {
      reason: "support evidence exists for this service story",
      repositoryCount: 1,
      repositoriesWithEvidence: 1,
      state: "partial",
      truncated: false
    },
    evidenceFamilies: ["support"],
    findings: [],
    nextCalls: [],
    repositories: []
  },
  lanes: [],
  name: "catalog-api",
  relationshipClusters: [],
  relationshipCounts: { downstream: 0, graphDependents: 0, references: 0, upstream: 0 },
  repoName: "catalog-api",
  summary: "catalog-api service story.",
  support: {
    ambiguousCount: 1,
    evidence: [
      {
        factId: "jira-123",
        factKind: "work_item.record",
        issueType: "Incident",
        label: "Jira PAY-123",
        provider: "jira_cloud",
        scopeId: "jira:site:example",
        sourceSystem: "jira",
        sourceUrlText: "https://jira.example.test/browse/PAY-123",
        status: "In Progress"
      },
      {
        factId: "pd-service",
        factKind: "incident_routing.observed_pagerduty_service",
        label: "PagerDuty routing",
        outcome: "exact",
        provider: "pagerduty",
        scopeId: "pagerduty:prod",
        sourceSystem: "pagerduty",
        status: "active"
      }
    ],
    evidenceCount: 2,
    incidentRoutingCount: 1,
    missingEvidence: [],
    truncated: false,
    workItemCount: 1
  },
  trafficPaths: [],
  trust: { basis: "hybrid", freshness: "fresh", level: "derived", profile: "production" }
};
