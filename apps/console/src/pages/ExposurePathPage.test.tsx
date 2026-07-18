import { fireEvent, render, screen } from "@testing-library/react";
import { act } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ExposurePathPage } from "./ExposurePathPage";
import { EshuApiHttpError, type EshuApiClient } from "../api/client";

// ExposurePathPage is the entrypoint-first exposure view (#3403). It must:
// - auto-load the proven ingress chain for a service deep-linked via ?service=
// - render posture tiles (public entrypoints, hops, WAF coverage, TLS termination)
// - render clickable hops, each opening an evidence panel with a truth level
// - never draw an "Internet" origin for an internal entrypoint
// - keep the handler-trace form available as advanced mode
describe("ExposurePathPage", () => {
  it("resolves a human catalog name to the canonical workload handle before tracing", async () => {
    const requests: string[] = [];
    const client = {
      get: async (path: string) => {
        requests.push(path);
        return { data: publicContext(), error: null, truth: null };
      },
      postJson: async () => {
        throw new Error("catalog matches must not call the resolver");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("combobox", { name: "Service selection" }), {
      target: { value: "Checkout API" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(requests).toEqual(["/api/v0/services/workload%3Acheckout/context"]);
  });

  it("restores a canonical deep link while showing the human service name", async () => {
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/workload%3Acheckout/context");
        return { data: publicContext(), error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
        <LocationSearch />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue("Checkout API");

    fireEvent.change(screen.getByRole("combobox", { name: "Service selection" }), {
      target: { value: "Payments API" },
    });
    expect(screen.getByTestId("location-search")).toBeEmptyDOMElement();
  });

  it("offers authorized services through a searchable accessible selector", async () => {
    const get = vi.fn(async (_path: string, _options?: { readonly signal?: AbortSignal }) =>
      contextEnvelope(),
    );
    const client = { get } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    const selector = screen.getByRole("combobox", { name: "Service selection" });
    fireEvent.focus(selector);
    fireEvent.change(selector, { target: { value: "payments-service" } });
    expect(screen.getByRole("listbox", { name: "Authorized services" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /Payments API/ }));
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(get.mock.calls[0]?.[0]).toBe("/api/v0/services/workload%3Apayments/context");
    expect(get.mock.calls[0]?.[1]?.signal).toBeInstanceOf(AbortSignal);
  });

  it("shows an ambiguous selector state and does not trace a guessed service", async () => {
    const get = vi.fn();
    const client = { get, postJson: vi.fn() } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage
          client={client}
          services={[
            { ...serviceOptions()[0], id: "workload:checkout-us", repo: "checkout-us" },
            { ...serviceOptions()[0], id: "workload:checkout-eu", repo: "checkout-eu" },
          ]}
        />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("combobox", { name: "Service selection" }), {
      target: { value: "Checkout API" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Multiple authorized services match",
    );
    expect(get).not.toHaveBeenCalled();
  });

  it("distinguishes no match and authorization failures in selector state", async () => {
    const postJson = vi
      .fn()
      .mockResolvedValueOnce({ count: 0, entities: [], limit: 10, truncated: false })
      .mockRejectedValueOnce(new EshuApiHttpError(403));
    const client = { get: vi.fn(), postJson } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={[]} />
      </MemoryRouter>,
    );

    const selector = screen.getByRole("combobox", { name: "Service selection" });
    fireEvent.change(selector, { target: { value: "missing-service" } });
    expect(selector).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No authorized service matches");

    fireEvent.change(selector, { target: { value: "restricted-service" } });
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("not authorized");
  });

  it("clears stale ingress immediately and ignores an older response after selection changes", async () => {
    let resolveCheckout: ((value: ReturnType<typeof contextEnvelope>) => void) | undefined;
    const checkoutResponse = new Promise<ReturnType<typeof contextEnvelope>>((resolve) => {
      resolveCheckout = resolve;
    });
    const get = vi.fn(async (path: string) => {
      if (path.includes("workload%3Acheckout")) return checkoutResponse;
      return contextEnvelope({ name: "payments", hostname: "payments.example.test" });
    });
    const client = { get } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    const selector = screen.getByRole("combobox", { name: "Service selection" });
    fireEvent.change(selector, { target: { value: "Checkout API" } });
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    fireEvent.change(selector, { target: { value: "Payments API" } });
    expect(screen.queryByText("checkout.example.test")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));

    expect(await screen.findByText("payments.example.test")).toBeInTheDocument();
    await act(async () => {
      resolveCheckout?.(contextEnvelope());
      await checkoutResponse;
    });
    expect(screen.getByText("payments.example.test")).toBeInTheDocument();
    expect(screen.queryByText("checkout.example.test")).not.toBeInTheDocument();
  });

  it("auto-loads the ingress chain and posture tiles for a deep-linked service", async () => {
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/workload%3Acheckout/context");
        return {
          data: publicContext(),
          error: null,
          truth: {
            capability: "platform_impact.context_overview",
            level: "derived",
            profile: "production",
            freshness: { state: "fresh" },
          },
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
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
      get: async () => ({ data: publicContext(), error: null, truth: null }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    await screen.findByText("Ingress chain");
    fireEvent.click(screen.getByRole("button", { pressed: false, name: /checkout.example.test/ }));

    expect(await screen.findByText("Node")).toBeInTheDocument();
    expect(screen.getByText("Truth level")).toBeInTheDocument();
  });

  it("does not draw an Internet origin for an internal entrypoint", async () => {
    const client = {
      get: async () => ({ data: internalContext(), error: null, truth: null }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=internal-api"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
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
        truth: null,
      }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=ghost"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("No proven ingress chain")).toBeInTheDocument();
  });

  it("requires a service before tracing", () => {
    const client = {
      get: async () => ({ data: null, error: null, truth: null }),
    } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(
      screen.getByText("Choose an authorized service or paste a canonical workload:… handle."),
    ).toBeInTheDocument();
  });

  it("loads the ingress chain when client connects after mount (boot race)", async () => {
    // Simulate the saved-private-env boot race: the page mounts with
    // client=undefined, then the client becomes available after mount.
    const client = {
      get: async (path: string) => {
        expect(path).toBe("/api/v0/services/workload%3Acheckout/context");
        return {
          data: publicContext(),
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const { rerender } = render(
      <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
        <ExposurePathPage client={undefined} services={serviceOptions()} />
      </MemoryRouter>,
    );

    // At mount client is undefined — no load fires. The page shows the empty prompt.
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();

    // Client connects after mount (boot race resolves).
    await act(async () => {
      rerender(
        <MemoryRouter initialEntries={["/exposure?service=checkout"]}>
          <ExposurePathPage client={client} services={serviceOptions()} />
        </MemoryRouter>,
      );
    });

    // The canLoad effect must now trigger the load automatically.
    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByText("WAF coverage")).toBeInTheDocument();
  });

  it("keeps the handler-trace form available as advanced mode", async () => {
    const client = {
      get: async () => ({
        data: { name: "x", entrypoints: [], network_paths: [] },
        error: null,
        truth: null,
      }),
      post: async () => ({ data: null, error: null, truth: null }),
    } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} />
      </MemoryRouter>,
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
        reason: "ingress host maps to the eks runtime",
      },
    ],
    ingress_posture: {
      waf_coverage: "protected",
      tls_termination: "terminated",
      edge_count: 1,
      waf_protected: 1,
      tls_terminated: 1,
      reason: "observed across 1 internet-facing edge resource",
    },
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
        visibility: "internal",
      },
    ],
  };
}

function serviceOptions(): readonly {
  readonly environments: readonly string[];
  readonly freshness: "fresh";
  readonly id: string;
  readonly kind: string;
  readonly name: string;
  readonly repo: string;
  readonly truth: "exact";
}[] {
  return [
    {
      environments: ["production"],
      freshness: "fresh",
      id: "workload:checkout",
      kind: "service",
      name: "Checkout API",
      repo: "checkout-service",
      truth: "exact",
    },
    {
      environments: ["production"],
      freshness: "fresh",
      id: "workload:internal-api",
      kind: "service",
      name: "Internal API",
      repo: "internal-api",
      truth: "exact",
    },
    {
      environments: ["production"],
      freshness: "fresh",
      id: "workload:ghost",
      kind: "service",
      name: "Ghost",
      repo: "ghost-service",
      truth: "exact",
    },
    {
      environments: ["production"],
      freshness: "fresh",
      id: "workload:payments",
      kind: "service",
      name: "Payments API",
      repo: "payments-service",
      truth: "exact",
    },
  ];
}

function contextEnvelope(options: { readonly hostname?: string; readonly name?: string } = {}): {
  readonly data: Record<string, unknown>;
  readonly error: null;
  readonly truth: null;
} {
  const hostname = options.hostname ?? "checkout.example.test";
  return {
    data: {
      ...publicContext(),
      entrypoints: [{ type: "hostname", target: hostname, visibility: "public" }],
      name: options.name ?? "checkout",
      network_paths: [
        {
          from: hostname,
          from_type: "hostname",
          platform_kind: "eks",
          to: `${options.name ?? "checkout"}-eks`,
          to_type: "runtime_platform",
          visibility: "public",
        },
      ],
    },
    error: null,
    truth: null,
  };
}

function LocationSearch(): React.JSX.Element {
  return <output data-testid="location-search">{useLocation().search}</output>;
}
