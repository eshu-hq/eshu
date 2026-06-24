import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { vi } from "vitest";

import { ServiceReportPage } from "./ServiceReportPage";
import { EshuApiHttpError, type EshuApiClient } from "../api/client";
import type { EshuEnvelope } from "../api/envelope";
import type { ServiceInvestigationResponse } from "../api/serviceInvestigation";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

function liveModel() {
  return modelFromSnapshot(emptySnapshot("live"));
}

function modelWithService(name: string) {
  return modelFromSnapshot({
    ...emptySnapshot("live"),
    services: [{ id: `svc:${name}`, name, kind: "service", repo: `${name}-repo`, environments: [], truth: "exact", freshness: "fresh" }]
  });
}

function reportEnvelope(
  data: ServiceInvestigationResponse | null,
  truthState: "fresh" | "stale" = "fresh"
): EshuEnvelope<ServiceInvestigationResponse> {
  return {
    data,
    error: null,
    truth: data === null ? null : { capability: "service.investigation.read", freshness: { state: truthState }, level: "derived", profile: "local_authoritative" }
  };
}

function fullReport(): ServiceInvestigationResponse {
  return {
    coverage_summary: { state: "partial", reason: "only deploy evidence indexed", repository_count: 3, repositories_with_evidence_count: 1, truncated: true },
    evidence_families_found: ["deployment", "source"],
    investigation_findings: [{ family: "deployment", evidence_path: "deploy/ecs.tf", summary: "ECS service declared" }],
    recommended_next_calls: [
      { tool: "get_service_story", arguments: { workload_id: "payments" }, reason: "open the dependency graph" },
      { tool: "trace_deployment_chain", arguments: { service_id: "svc-1" }, reason: "trace the deploy chain" }
    ],
    repositories_with_evidence: [{ repo_name: "payments-repo", roles: ["service"], evidence_families: ["deployment"] }],
    service_story_path: "/api/v0/services/payments/story",
    service_context_path: "/api/v0/services/payments/context"
  };
}

function clientReturning(env: EshuEnvelope<ServiceInvestigationResponse>): { client: EshuApiClient; paths: string[] } {
  const paths: string[] = [];
  const client = {
    get: vi.fn(async (path: string) => {
      paths.push(path);
      return env;
    })
  } as unknown as EshuApiClient;
  return { client, paths };
}

function renderAt(path: string, client: EshuApiClient) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/service-report" element={<ServiceReportPage client={client} model={liveModel()} onOpenService={vi.fn()} />} />
        <Route path="/service-report/:serviceName" element={<ServiceReportPage client={client} model={liveModel()} onOpenService={vi.fn()} />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("ServiceReportPage", () => {
  it("renders the heading and a service input", () => {
    const { client } = clientReturning(reportEnvelope(fullReport()));
    renderAt("/service-report", client);
    expect(screen.getByRole("heading", { name: "Service intelligence report" })).toBeInTheDocument();
    expect(screen.getByLabelText("Service name")).toBeInTheDocument();
  });

  it("auto-loads a default catalog service on open when none is selected", async () => {
    const { client, paths } = clientReturning(reportEnvelope(fullReport()));
    render(
      <MemoryRouter initialEntries={["/service-report"]}>
        <Routes>
          <Route path="/service-report" element={<ServiceReportPage client={client} model={modelWithService("acme-app")} onOpenService={vi.fn()} />} />
          <Route path="/service-report/:serviceName" element={<ServiceReportPage client={client} model={modelWithService("acme-app")} onOpenService={vi.fn()} />} />
        </Routes>
      </MemoryRouter>
    );
    await waitFor(() => expect(paths).toEqual(["/api/v0/investigations/services/acme-app"]));
    expect(await screen.findByText("payments-repo")).toBeInTheDocument();
  });

  it("deep-loads a report and renders coverage, findings, and scope", async () => {
    const { client, paths } = clientReturning(reportEnvelope(fullReport()));
    renderAt("/service-report/payments", client);

    await waitFor(() => expect(paths).toEqual(["/api/v0/investigations/services/payments"]));
    expect(await screen.findByText(/partial/i)).toBeInTheDocument();
    expect(screen.getByText("only deploy evidence indexed")).toBeInTheDocument();
    expect(screen.getByText("ECS service declared")).toBeInTheDocument();
    expect(screen.getByText("payments-repo")).toBeInTheDocument();
    expect(screen.getByText(/truncated/i)).toBeInTheDocument();
    // Link into the graph view for the same service.
    expect(screen.getByRole("link", { name: /evidence graph/i })).toHaveAttribute("href", "/service-story/payments");
  });

  it("makes a routable suggested investigation clickable and leaves an unroutable one inert", async () => {
    const { client } = clientReturning(reportEnvelope(fullReport()));
    renderAt("/service-report/payments", client);

    const investigations = await screen.findByTestId("suggested-investigations");
    // get_service_story routes to the graph view.
    expect(within(investigations).getByRole("link", { name: /open the dependency graph/i })).toHaveAttribute(
      "href",
      "/service-story/payments"
    );
    // trace_deployment_chain has no console destination — shown but not a link.
    expect(within(investigations).getByText("trace the deploy chain")).toBeInTheDocument();
    expect(within(investigations).queryByRole("link", { name: /trace the deploy chain/i })).toBeNull();
  });

  it("clears a stale report when navigating back to the bare route", async () => {
    const { client } = clientReturning(reportEnvelope(fullReport()));
    function Nav(): React.JSX.Element {
      const navigate = useNavigate();
      return <button onClick={() => navigate("/service-report")} type="button">to bare</button>;
    }
    render(
      <MemoryRouter initialEntries={["/service-report/payments"]}>
        <Routes>
          <Route path="/service-report" element={<><Nav /><ServiceReportPage client={client} model={liveModel()} onOpenService={vi.fn()} /></>} />
          <Route path="/service-report/:serviceName" element={<><Nav /><ServiceReportPage client={client} model={liveModel()} onOpenService={vi.fn()} /></>} />
        </Routes>
      </MemoryRouter>
    );
    expect(await screen.findByText("ECS service declared")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "to bare" }));
    await waitFor(() => expect(screen.queryByText("ECS service declared")).not.toBeInTheDocument());
  });

  it("shows an empty state when the report has no evidence", async () => {
    const empty: ServiceInvestigationResponse = { coverage_summary: { state: "unknown" } };
    const { client } = clientReturning(reportEnvelope(empty));
    renderAt("/service-report/payments", client);
    expect(await screen.findByText(/No investigation evidence/i)).toBeInTheDocument();
  });

  it("keeps a stale freshness state visible", async () => {
    const { client } = clientReturning(reportEnvelope(fullReport(), "stale"));
    renderAt("/service-report/payments", client);
    expect(await screen.findByText("stale")).toBeInTheDocument();
  });

  it("shows an API failure without rendering stale report content", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: { code: "unavailable", message: "investigation capability disabled" },
        truth: null
      }))
    } as unknown as EshuApiClient;

    renderAt("/service-report/payments", client);
    expect(await screen.findByText("unavailable: investigation capability disabled")).toBeInTheDocument();
    expect(screen.queryByText("ECS service declared")).not.toBeInTheDocument();
  });

  it("renders the error state when the client throws a non-2xx error", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new EshuApiHttpError(404, { code: "not_found", message: "service not found" });
      })
    } as unknown as EshuApiClient;

    renderAt("/service-report/ghost", client);
    expect(await screen.findByText("not_found: service not found")).toBeInTheDocument();
    expect(screen.queryByText("ECS service declared")).not.toBeInTheDocument();
  });
});
