import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { App } from "./App";

describe("App shell", () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("renders the redesigned console navigation", async () => {
    // The shell auto-connects to the API on boot; make it unreachable so it
    // falls back to the demo model without persisting anything.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("offline in test");
      })
    );

    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>
    );

    expect(
      screen.getByRole("heading", { name: "Eshu Console" })
    ).toBeInTheDocument();

    // findBy lets the boot connect attempt settle before assertions.
    expect(
      await screen.findByRole("link", { name: "Dashboard" })
    ).toHaveAttribute("href", "/dashboard");
    expect(
      screen.getByRole("link", { name: "Graph Explorer" })
    ).toHaveAttribute("href", "/explorer");
    expect(screen.getByRole("link", { name: "Catalog" })).toHaveAttribute(
      "href",
      "/catalog"
    );
    expect(screen.getByRole("link", { name: "Findings" })).toHaveAttribute(
      "href",
      "/findings"
    );
    expect(
      screen.getByRole("link", { name: "Vulnerabilities" })
    ).toHaveAttribute("href", "/vulnerabilities");
    expect(screen.getByRole("link", { name: "Operations" })).toHaveAttribute(
      "href",
      "/operations"
    );
    expect(
      screen.getByRole("link", { name: /context graph/i })
    ).toHaveAttribute("href", "/");
  });

  it("routes service workspaces through the Service Atlas support surface", async () => {
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ repository_count: 1, status: "ready" });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1, workload_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [], workloads: [] } });
      }
      if (path === "/eshu-api/api/v0/services/api-node-boats/story") {
        return Response.json({
          api_surface: { endpoint_count: 1, endpoints: [], method_count: 1 },
          service_identity: {
            repo_name: "api-node-boats",
            service_name: "api-node-boats"
          },
          support_overview: {
            target_support: {
              evidence: [{
                fact_id: "jira-123",
                fact_kind: "work_item.record",
                payload: {
                  provider: "jira_cloud",
                  status_name: "In Progress",
                  url_redacted: "https://jira.example.test/browse/PAY-123",
                  work_item_key: "PAY-123"
                }
              }],
              evidence_count: 1,
              incident_routing_count: 0,
              work_item_count: 1
            }
          }
        });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/workspace/services/api-node-boats"]}>
        <App />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Service Atlas" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Service Atlas" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Impact review" }));
    expect(screen.getByRole("region", { name: "Incidents and issues" })).toBeInTheDocument();
    expect(screen.getByText("Jira PAY-123")).toBeInTheDocument();
  });
});
