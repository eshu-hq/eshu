import { StrictMode } from "react";
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
    // renders the explicit unavailable model without persisting anything.
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
    expect(screen.getByPlaceholderText("Search repos, services, CVEs, evidence…")).toBeInTheDocument();
    expect(screen.getByText("Overview")).toBeInTheDocument();
    expect(screen.getByText("Inventory")).toBeInTheDocument();
    expect(screen.getByText("Cloud & Telemetry")).toBeInTheDocument();

    // findBy lets the boot connect attempt settle before assertions.
    expect(
      await screen.findByRole("link", { name: "Dashboard" })
    ).toHaveAttribute("href", "/dashboard");
    expect(screen.getByRole("link", { name: "Ask Eshu" })).toHaveAttribute(
      "href",
      "/ask"
    );
    expect(
      screen.getByRole("link", { name: "Graph Explorer" })
    ).toHaveAttribute("href", "/explorer");
    expect(screen.getByRole("link", { name: "Impact" })).toHaveAttribute(
      "href",
      "/impact"
    );
    expect(screen.getByRole("link", { name: "Changed Since" })).toHaveAttribute(
      "href",
      "/changed-since"
    );
    expect(screen.getByRole("link", { name: "Dead code" })).toHaveAttribute(
      "href",
      "/dead-code"
    );
    expect(screen.getByRole("link", { name: "Code graph" })).toHaveAttribute(
      "href",
      "/code-graph"
    );
    expect(screen.getByRole("link", { name: "Catalog" })).toHaveAttribute(
      "href",
      "/catalog"
    );
    expect(screen.getByRole("link", { name: "Topology" })).toHaveAttribute(
      "href",
      "/topology"
    );
    expect(screen.getByRole("link", { name: "Incidents" })).toHaveAttribute(
      "href",
      "/incidents"
    );
    expect(screen.getByRole("link", { name: "CI/CD" })).toHaveAttribute(
      "href",
      "/ci-cd/run-correlations"
    );
    expect(screen.getByRole("link", { name: "Cloud Drift" })).toHaveAttribute(
      "href",
      "/cloud-drift"
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
    expect(screen.getByRole("link", { name: "Collector Readiness" })).toHaveAttribute(
      "href",
      "/collector-readiness"
    );
    expect(screen.getByRole("link", { name: "Surface Inventory" })).toHaveAttribute(
      "href",
      "/surface-inventory"
    );
    expect(
      screen.getByRole("link", { name: /context graph/i })
    ).toHaveAttribute("href", "/");
  });

  it("registers the replatforming console route", async () => {
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
      await screen.findByRole("link", { name: "Replatforming" })
    ).toHaveAttribute("href", "/replatforming");
  });

  it("routes cloud drift to the live drift readback page", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("offline in test");
      })
    );

    render(
      <MemoryRouter initialEntries={["/cloud-drift"]}>
        <App />
      </MemoryRouter>
    );

    expect(
      await screen.findByRole("link", { name: "Cloud Drift" })
    ).toHaveAttribute("href", "/cloud-drift");
  });

  it("registers the changed-since console route", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("offline in test");
      })
    );

    render(
      <MemoryRouter initialEntries={["/changed-since"]}>
        <App />
      </MemoryRouter>
    );

    expect(
      await screen.findByRole("link", { name: "Changed Since" })
    ).toHaveAttribute("href", "/changed-since");
  });

  it("registers the secrets IAM posture route", async () => {
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
      await screen.findByRole("link", { name: "Secrets/IAM" })
    ).toHaveAttribute("href", "/secrets-iam");
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
      if (path === "/eshu-api/api/v0/services/catalog-api/story") {
        return Response.json({
          api_surface: { endpoint_count: 1, endpoints: [], method_count: 1 },
          service_identity: {
            repo_name: "catalog-api",
            service_name: "catalog-api"
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
      <MemoryRouter initialEntries={["/workspace/services/catalog-api"]}>
        <App />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Service Atlas" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Service Atlas" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Impact review" }));
    expect(screen.getByRole("region", { name: "Incidents and issues" })).toBeInTheDocument();
    expect(screen.getByText("Jira PAY-123")).toBeInTheDocument();
  });

  it("boots the saved connection exactly once under StrictMode and populates the catalog", async () => {
    // Issue #1727: StrictMode's dev double-invoke must not fire two concurrent
    // boot connects whose in-flight fetches abort each other and blank out the
    // Catalog. The bootedRef guard means the catalog (services) endpoint is hit
    // once and the Catalog page reliably shows the live service.
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    let catalogCalls = 0;
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/eshu-api/api/v0/catalog") {
        catalogCalls += 1;
        return Response.json({
          data: { services: [{ id: "workload:api", name: "api", kind: "service", repo_name: "api" }] }
        });
      }
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      return Response.json({ data: {} });
    }));

    render(
      <StrictMode>
        <MemoryRouter initialEntries={["/catalog"]}>
          <App />
        </MemoryRouter>
      </StrictMode>
    );

    // The Catalog page renders the live service once the boot connect resolves.
    // "api" appears in both the name and repository cells, so assert on the
    // entries header (1 entry) and the absence of the empty state instead.
    expect(await screen.findByText("1 entries")).toBeInTheDocument();
    expect(screen.queryByText("No catalog entries from this source.")).not.toBeInTheDocument();
    expect(screen.getAllByText("api").length).toBeGreaterThan(0);
    // StrictMode double-invokes effects in dev; the boot guard keeps the catalog
    // fetch to a single call so the requests cannot abort each other.
    expect(catalogCalls).toBe(1);
  });

  it("toggles the demo-style verified evidence filter over live findings", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = new Request(input);
      const path = new URL(request.url).pathname;
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      if (path === "/eshu-api/api/v0/code/dead-code") {
        return Response.json({
          data: {
            results: [{
              classification: "unused",
              entity_id: "content-entity:e1",
              file_path: "server/routes.ts",
              name: "legacyRoute",
              repo_id: "repository:r1",
              start_line: 12
            }]
          },
          truth: { level: "fallback", freshness: { state: "fresh" }, profile: "production" }
        });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/findings"]}>
        <App />
      </MemoryRouter>
    );

    expect(await screen.findByText("Unreferenced symbol legacyRoute")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show verified evidence only" }));

    expect(screen.queryByText("Unreferenced symbol legacyRoute")).not.toBeInTheDocument();
    expect(screen.getByText("No findings from this source.")).toBeInTheDocument();
  });

  it("counts vulnerabilities in the Findings navigation worklist badge", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const request = new Request(input);
      const path = new URL(request.url).pathname;
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      if (path === "/eshu-api/api/v0/code/dead-code") {
        return Response.json({
          data: {
            results: [{
              classification: "unused",
              entity_id: "content-entity:e1",
              file_path: "server/routes.ts",
              name: "legacyRoute",
              repo_id: "repository:r1"
            }]
          },
          truth: { level: "derived", freshness: { state: "fresh" }, profile: "production" }
        });
      }
      if (path === "/eshu-api/api/v0/supply-chain/impact/findings") {
        return Response.json({
          data: {
            findings: [{
              advisory_id: "CVE-2026-1234",
              cvss_score: 8.1,
              package_name: "lodash",
              repository_id: "repository:r1"
            }]
          }
        });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/findings"]}>
        <App />
      </MemoryRouter>
    );

    expect(await screen.findByText("CVE-2026-1234 · lodash")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Findings" }).querySelector(".nav-count")).toHaveTextContent("2");
  });

  it("routes CVE searches to vulnerability detail instead of graph explorer", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      if (path === "/eshu-api/api/v0/supply-chain/impact/findings") {
        return Response.json({
          data: {
            findings: [{
              advisory_id: "CVE-2024-0001",
              cvss_score: 8.1,
              package_name: "sample-lib",
              repository_id: "repository:r1"
            }]
          }
        });
      }
      if (path === "/eshu-api/api/v0/supply-chain/vulnerabilities/CVE-2024-0001") {
        return Response.json({
          data: {
            canonical_id: "CVE-2024-0001",
            cve_ids: ["CVE-2024-0001"],
            sources: [{ cvss_score: 8.1, severity_label: "high" }],
            affected_packages: [{ package_name: "sample-lib", fixed_version: "2.0.1" }]
          }
        });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <App />
      </MemoryRouter>
    );

    const input = await screen.findByLabelText("Search Eshu");
    fireEvent.change(input, { target: { value: "CVE-2024-0001" } });
    const form = input.closest("form");
    expect(form).not.toBeNull();
    fireEvent.submit(form!);

    expect(await screen.findByRole("heading", { name: "CVE-2024-0001" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Graph Explorer" })).not.toBeInTheDocument();
  });

  it("routes repository searches to the source browser instead of graph explorer", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "private", apiBaseUrl: "/eshu-api/", recentApiBaseUrls: ["/eshu-api/"] })
    );
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/eshu-api/api/v0/index-status") {
        return Response.json({ status: "ready", repository_count: 1, queue: {} });
      }
      if (path === "/eshu-api/api/v0/ecosystem/overview") {
        return Response.json({ data: { repo_count: 1 } });
      }
      if (path === "/eshu-api/api/v0/catalog") {
        return Response.json({ data: { services: [] } });
      }
      if (path === "/eshu-api/api/v0/repositories") {
        return Response.json({
          data: { repositories: [{ id: "repository:helm-charts", name: "helm-charts", repo_slug: "platform/helm-charts" }] }
        });
      }
      if (path === "/eshu-api/api/v0/repositories/repository%3Ahelm-charts/tree") {
        return Response.json({
          data: { ref: "main", path: "", entries: [] }
        });
      }
      return Response.json({ data: {} });
    }));

    render(
      <MemoryRouter initialEntries={["/dashboard"]}>
        <App />
      </MemoryRouter>
    );

    const input = await screen.findByLabelText("Search Eshu");
    fireEvent.change(input, { target: { value: "helm-charts" } });
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });

    expect(await screen.findByRole("heading", { name: /helm-charts/ })).toBeInTheDocument();
    expect(screen.getByText(/File tree \+ viewer/)).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Graph Explorer" })).not.toBeInTheDocument();
  });
});
