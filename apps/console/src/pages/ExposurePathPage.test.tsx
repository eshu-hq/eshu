import { fireEvent, render, screen } from "@testing-library/react";
import { act, StrictMode } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ExposurePathPage } from "./ExposurePathPage";
import { contextEnvelope, publicContext, serviceOptions } from "./ExposurePathPageTestFixtures";
import { EshuApiHttpError, type EshuApiClient } from "../api/client";

// ExposurePathPage is the entrypoint-first exposure view (#3403). It must:
// - auto-load the proven ingress chain for a service deep-linked via ?service=
// - render posture tiles (public entrypoints, hops, WAF coverage, TLS termination)
// - render clickable hops, each opening an evidence panel with a truth level
// - never draw an "Internet" origin for an internal entrypoint
// - keep the handler-trace form available as advanced mode
describe("ExposurePathPage", () => {
  it("resolves a human catalog name authoritatively before tracing", async () => {
    const requests: string[] = [];
    const client = {
      get: async (path: string) => {
        requests.push(path);
        return { data: publicContext(), error: null, truth: null };
      },
      postJson: async () => ({
        count: 1,
        entities: [
          {
            id: "workload:checkout",
            labels: ["Workload"],
            name: "Checkout API",
            repo_name: "checkout-service",
          },
        ],
        limit: 10,
        truncated: false,
      }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByRole("combobox", { name: "Service selection" }), {
      target: { value: "Checkout API" },
    });
    const traceButton = screen.getByRole("button", { name: "Trace ingress" });
    fireEvent.focus(screen.getByRole("combobox", { name: "Service selection" }));
    expect(screen.getByRole("listbox", { name: "Authorized services" })).toBeInTheDocument();
    fireEvent.blur(screen.getByRole("combobox", { name: "Service selection" }), {
      relatedTarget: traceButton,
    });
    fireEvent.click(traceButton);

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.queryByRole("listbox", { name: "Authorized services" })).not.toBeInTheDocument();
    expect(requests).toEqual(["/api/v0/services/workload%3Acheckout/context"]);
  });

  it("restores a canonical deep link while showing the human service name", async () => {
    const get = vi.fn(async (path: string) => {
      expect(path).toBe("/api/v0/services/workload%3Acheckout/context");
      return { data: publicContext(), error: null, truth: null };
    });
    const client = {
      get,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
        <LocationSearch />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue("Checkout API");

    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(2);

    fireEvent.change(screen.getByRole("combobox", { name: "Service selection" }), {
      target: { value: "Payments API" },
    });
    expect(screen.getByTestId("location-search")).toBeEmptyDOMElement();
  });

  it("clears stale service truth when history removes the active deep link", async () => {
    const client = {
      get: vi.fn(async () => ({ data: publicContext(), error: null, truth: null })),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
        <RemoveServiceParam />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove service parameter" }));

    expect(
      await screen.findByText("Enter an internet-facing service to trace its ingress chain."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue("");
  });

  it("restarts an aborted deep-link request during the StrictMode effect rehearsal", async () => {
    const get = vi.fn(async () => contextEnvelope());
    const client = { get } as unknown as EshuApiClient;

    render(
      <StrictMode>
        <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
          <ExposurePathPage client={client} services={serviceOptions()} />
        </MemoryRouter>
      </StrictMode>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Trace ingress" })).toBeEnabled();
    expect(get).toHaveBeenCalled();
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

  it("supports keyboard selection with an active descendant and Escape dismissal", () => {
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={{} as EshuApiClient} services={serviceOptions()} />
      </MemoryRouter>,
    );

    const selector = screen.getByRole("combobox", { name: "Service selection" });
    fireEvent.focus(selector);
    fireEvent.keyDown(selector, { key: "ArrowDown" });
    expect(selector).toHaveAttribute("aria-activedescendant");
    expect(selector.getAttribute("aria-activedescendant")).not.toBe("");
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("Checkout API");
    fireEvent.keyDown(selector, { key: "Enter" });
    expect(selector).toHaveValue("Checkout API");
    expect(screen.queryByRole("listbox", { name: "Authorized services" })).not.toBeInTheDocument();

    fireEvent.focus(selector);
    fireEvent.keyDown(selector, { key: "Escape" });
    expect(screen.queryByRole("listbox", { name: "Authorized services" })).not.toBeInTheDocument();
  });

  it("discloses that the visible authorized service catalog is truncated", () => {
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage
          catalogTruncated
          client={{} as EshuApiClient}
          services={serviceOptions()}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText(/visible service list is bounded/i)).toBeInTheDocument();
    expect(screen.getByText(/submit-time resolver searches beyond it/i)).toBeInTheDocument();
  });

  it("shows an ambiguous selector state and does not trace a guessed service", async () => {
    const get = vi.fn();
    const client = {
      get,
      postJson: vi.fn(async () => ({
        count: 2,
        entities: [
          {
            id: "workload:checkout-us",
            labels: ["Workload"],
            name: "Checkout API",
            repo_name: "checkout-us",
          },
          {
            id: "workload:checkout-eu",
            labels: ["Workload"],
            name: "Checkout API",
            repo_name: "checkout-eu",
          },
        ],
        limit: 10,
        truncated: false,
      })),
    } as unknown as EshuApiClient;
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

  it("distinguishes no match from a request-level authorization failure", async () => {
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
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "active session is not authorized to use service resolution",
    );
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
    const client = {
      get,
      postJson: vi.fn(async (_path: string, body: { readonly name: string }) => ({
        count: 1,
        entities: [
          {
            id: body.name === "Payments API" ? "workload:payments" : "workload:checkout",
            labels: ["Workload"],
            name: body.name,
          },
        ],
        limit: 10,
        truncated: false,
      })),
    } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/exposure"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    const selector = screen.getByRole("combobox", { name: "Service selection" });
    fireEvent.change(selector, { target: { value: "Checkout API" } });
    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(selector).toBeEnabled();
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
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ExposurePathPage client={undefined} services={serviceOptions()} />
      </MemoryRouter>,
    );

    // At mount client is undefined — no load fires. The page shows the empty prompt.
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();

    // Client connects after mount (boot race resolves).
    await act(async () => {
      rerender(
        <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
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

function LocationSearch(): React.JSX.Element {
  return <output data-testid="location-search">{useLocation().search}</output>;
}

function RemoveServiceParam(): React.JSX.Element {
  const navigate = useNavigate();
  return (
    <button onClick={() => navigate("/exposure")} type="button">
      Remove service parameter
    </button>
  );
}
