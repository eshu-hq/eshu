import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "../api/client";
import { ExposurePathPage } from "./ExposurePathPage";
import { act } from "react";

// ExposurePathPage is the entrypoint-first exposure view (#3403). It must:
// - auto-load the proven ingress chain for a service deep-linked via ?service=
// - render posture tiles (public entrypoints, hops, WAF coverage, TLS termination)
// - render clickable hops, each opening an evidence panel with a truth level
// - never draw an "Internet" origin for an internal entrypoint
// - keep the handler-trace form available as advanced mode
describe("ExposurePathPage", () => {
  it("auto-loads the ingress chain and posture tiles for a deep-linked service", async () => {
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/checkout/context");
        return {
          data: publicContext(),
          error: null,
          truth: {
            capability: "platform_impact.context_overview",
            level: "derived",
            profile: "production",
            freshness: { state: "fresh" }
          }
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    // Posture tiles.
    expect(screen.getByText("Public entrypoints")).toBeInTheDocument();
    expect(screen.getByText("WAF coverage")).toBeInTheDocument();
    expect(screen.getByText("TLS termination")).toBeInTheDocument();
    // Proven Internet origin hop for an observed-public entrypoint.
    expect(screen.getByText("Internet")).toBeInTheDocument();
    expect(screen.getByText("checkout.example.test")).toBeInTheDocument();
  });

  it("opens hop evidence with a truth level when a hop is clicked", async () => {
    const client = {
      get: async () => ({ data: publicContext(), error: null, truth: null })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    await screen.findByText("Ingress chain");
    fireEvent.click(screen.getByRole("button", { pressed: false, name: /checkout.example.test/ }));

    expect(await screen.findByText("Node")).toBeInTheDocument();
    expect(screen.getByText("Truth level")).toBeInTheDocument();
  });

  it("does not draw an Internet origin for an internal entrypoint", async () => {
    const client = {
      get: async () => ({ data: internalContext(), error: null, truth: null })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=internal-api"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    await screen.findByText("Ingress chain");
    expect(screen.getByText("Network boundary")).toBeInTheDocument();
    expect(screen.queryByText("Internet")).not.toBeInTheDocument();
  });

  it("shows an honest empty state when no ingress path is proven", async () => {
    const client = {
      get: async () => ({
        data: { name: "ghost", entrypoints: [], network_paths: [] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=ghost"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText("No proven ingress chain")).toBeInTheDocument();
  });

  it("requires a service before tracing", () => {
    const client = { get: async () => ({ data: null, error: null, truth: null }) } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(screen.getByText("A service name is required to trace its ingress chain.")).toBeInTheDocument();
  });

  it("loads the ingress chain when client connects after mount (boot race)", async () => {
    // Simulate the saved-private-env boot race: the page mounts with
    // client=undefined, then the client becomes available after mount.
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/checkout/context");
        return {
          data: publicContext(),
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    const { rerender } = render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={undefined} />
      </MemoryRouter>
    );

    // At mount client is undefined — no load fires. The page shows the empty prompt.
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();

    // Client connects after mount (boot race resolves).
    await act(async () => {
      rerender(
        <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
          <ExposurePathPage client={client} />
        </MemoryRouter>
      );
    });

    // The canLoad effect must now trigger the load automatically.
    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByText("WAF coverage")).toBeInTheDocument();
  });

  it("keeps the handler-trace form available as advanced mode", async () => {
    const client = {
      get: async () => ({ data: { name: "x", entrypoints: [], network_paths: [] }, error: null, truth: null }),
      post: async () => ({ data: null, error: null, truth: null })
    } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>
    );
    fireEvent.click(screen.getByText("Advanced: handler trace"));
    expect(await screen.findByRole("button", { name: "Trace exposure" })).toBeInTheDocument();
    expect(screen.getByLabelText("Source handler name")).toBeInTheDocument();
  });
});

function publicContext(): Record<string, unknown> {
  return {
    name: "checkout",
    entrypoints: [{ type: "hostname", target: "checkout.example.test", visibility: "public" }],
    network_paths: [
      {
        path_type: "hostname_to_runtime",
        from_type: "hostname",
        from: "checkout.example.test",
        to_type: "runtime_platform",
        to: "checkout-eks",
        platform_kind: "eks",
        environment: "production",
        visibility: "public",
        reason: "ingress host maps to the eks runtime"
      }
    ],
    ingress_posture: {
      waf_coverage: "protected",
      tls_termination: "terminated",
      edge_count: 1,
      waf_protected: 1,
      tls_terminated: 1,
      reason: "observed across 1 internet-facing edge resource"
    }
  };
}

function internalContext(): Record<string, unknown> {
  return {
    name: "internal-api",
    entrypoints: [{ type: "docs_route", target: "/internal/health", visibility: "internal" }],
    network_paths: [
      {
        path_type: "docs_route_to_runtime",
        from_type: "docs_route",
        from: "/internal/health",
        to_type: "runtime_platform",
        to: "internal-eks",
        platform_kind: "eks",
        visibility: "internal"
      }
    ]
  };
}
