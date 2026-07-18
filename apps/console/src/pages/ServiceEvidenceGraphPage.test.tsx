import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { vi } from "vitest";

import { ServiceEvidenceGraphPage } from "./ServiceEvidenceGraphPage";
import {
  clientFor,
  deriveEnvelope,
  liveModel,
  modelWithService,
  renderServiceEvidenceGraphAt,
  supportedPacket,
} from "./ServiceEvidenceGraphPage.testSupport";
import type { EshuApiClient } from "../api/client";

describe("ServiceEvidenceGraphPage", () => {
  it("renders the heading and a service input", () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderServiceEvidenceGraphAt("/service-story", client);
    expect(screen.getByRole("heading", { name: "Service evidence graph" })).toBeInTheDocument();
    expect(screen.getByLabelText("Service name")).toBeInTheDocument();
  });

  it("auto-loads a default catalog service on open when none is selected", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    render(
      <MemoryRouter initialEntries={["/service-story"]}>
        <Routes>
          <Route
            path="/service-story"
            element={
              <ServiceEvidenceGraphPage
                client={client}
                model={modelWithService("acme-app")}
                onOpenService={vi.fn()}
              />
            }
          />
          <Route
            path="/service-story/:serviceName"
            element={
              <ServiceEvidenceGraphPage
                client={client}
                model={modelWithService("acme-app")}
                onOpenService={vi.fn()}
              />
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(paths).toEqual(["/api/v0/services/acme-app/story", "/api/v0/visualizations/derive"]);
    });
    expect(await screen.findByText("billing")).toBeInTheDocument();
  });

  it("deep-loads a service story packet and renders nodes with truth labels", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    await waitFor(() => {
      expect(paths).toEqual(["/api/v0/services/payments/story", "/api/v0/visualizations/derive"]);
    });
    expect(await screen.findByText("billing")).toBeInTheDocument();
    expect(screen.getAllByText("payments").length).toBeGreaterThan(0);
    // Truth label is rendered as text, not color alone.
    expect(screen.getByText("visualization.derive")).toBeInTheDocument();
    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    expect(screen.getByText("fresh")).toBeInTheDocument();
    // Bounded counts are visible so a partial subgraph is never read as complete.
    expect(screen.getByText(/of up to 60 nodes/)).toBeInTheDocument();
  });

  it("submits the form into a deep-linkable service story route", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    renderServiceEvidenceGraphAt("/service-story", client);

    fireEvent.change(screen.getByLabelText("Service name"), { target: { value: "payments" } });
    fireEvent.click(screen.getByRole("button", { name: "Show evidence graph" }));

    await waitFor(() => {
      expect(paths).toEqual(["/api/v0/services/payments/story", "/api/v0/visualizations/derive"]);
    });
  });

  it("shows a story error without rendering a stale graph", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "service not found" },
        truth: null,
      })),
      post: vi.fn(),
    } as unknown as EshuApiClient;

    renderServiceEvidenceGraphAt("/service-story/ghost", client);
    expect(await screen.findByText("not_found: service not found")).toBeInTheDocument();
    expect(screen.queryByText("billing")).not.toBeInTheDocument();
  });

  it("clears a stale graph when navigating back to the bare route", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    function Nav(): React.JSX.Element {
      const navigate = useNavigate();
      return (
        <button onClick={() => navigate("/service-story")} type="button">
          to bare
        </button>
      );
    }
    render(
      <MemoryRouter initialEntries={["/service-story/payments"]}>
        <Routes>
          <Route
            path="/service-story"
            element={
              <>
                <Nav />
                <ServiceEvidenceGraphPage
                  client={client}
                  model={liveModel()}
                  onOpenService={vi.fn()}
                />
              </>
            }
          />
          <Route
            path="/service-story/:serviceName"
            element={
              <>
                <Nav />
                <ServiceEvidenceGraphPage
                  client={client}
                  model={liveModel()}
                  onOpenService={vi.fn()}
                />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText("billing")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "to bare" }));
    await waitFor(() => expect(screen.queryByText("billing")).not.toBeInTheDocument());
  });

  it("selects a node and opens the inline evidence panel", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    const billing = await screen.findByText("billing");
    fireEvent.click(billing);

    const panel = await screen.findByRole("region", { name: /Evidence for billing/i });
    expect(within(panel).getByText("billing")).toBeInTheDocument();
    expect(within(panel).getByText(/upstream/)).toBeInTheDocument();
  });

  it("selects an evidence-lane pill and opens the inline evidence panel", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    await screen.findByText("billing");
    fireEvent.click(screen.getByRole("button", { name: /DEPENDS_ON/ }));

    const panel = await screen.findByRole("region", { name: /Evidence for DEPENDS_ON/i });
    expect(within(panel).getByText("DEPENDS_ON")).toBeInTheDocument();
  });

  it("renders human relationship narratives with raw visualization ids as secondary diagnostics", async () => {
    const packet = supportedPacket({
      nodes: [
        {
          id: "viznode:service",
          type: "service",
          label: "api-node-boats",
          category: "service",
          role: "workload",
        },
        {
          id: "viznode:argocd",
          type: "repository",
          label: "iac-eks-argocd",
          category: "deployment",
          role: "deployment_configuration",
        },
        {
          id: "viznode:multi-role",
          type: "repository",
          label: "terraform-stack-user-management",
          category: "deployment",
          role: "deployment_configuration",
        },
      ],
      edges: [
        {
          id: "vizedge:provisions",
          source: "viznode:argocd",
          target: "viznode:service",
          relationship: "PROVISIONING_SOURCE_CHAIN",
          truth_label: "exact",
        },
        {
          id: "vizedge:consumed-by",
          source: "viznode:service",
          target: "viznode:multi-role",
          relationship: "CONSUMED_BY",
        },
      ],
    });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/api-node-boats", client);

    expect(
      await screen.findByText(
        "iac-eks-argocd (deployment configuration repository) provisions api-node-boats (workload service)",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "api-node-boats (workload service) is consumed by terraform-stack-user-management (downstream repository)",
      ),
    ).toBeInTheDocument();
    const diagnostics = screen.getAllByLabelText("Relationship diagnostic IDs");
    expect(diagnostics[0]).toHaveTextContent("viznode:argocd → viznode:service");
  });
});
