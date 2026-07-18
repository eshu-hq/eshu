import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { ExposurePathPage } from "./ExposurePathPage";
import { internalContext, publicContext, serviceOptions } from "./ExposurePathPageTestFixtures";
import type { EshuApiClient } from "../api/client";

describe("ExposurePathPage ingress presentation", () => {
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
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    expect(screen.getByText("Public entrypoints")).toBeInTheDocument();
    expect(screen.getByText("WAF coverage")).toBeInTheDocument();
    expect(screen.getByText("TLS termination")).toBeInTheDocument();
    expect(screen.getByText("Internet")).toBeInTheDocument();
    expect(screen.getByText("checkout.example.test")).toBeInTheDocument();
    expect(screen.getByText("checkout.example.test · public · production")).toBeInTheDocument();
  });

  it("opens hop evidence with a truth level when a hop is clicked", async () => {
    const client = {
      get: async () => ({ data: publicContext(), error: null, truth: null }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
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
      <MemoryRouter initialEntries={["/exposure?service=workload%3Ainternal-api"]}>
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
        data: {
          name: "ghost",
          entrypoints: [],
          network_paths: [],
          ingress_posture: {
            waf_coverage: "unproven",
            tls_termination: "unproven",
            edge_count: 0,
          },
        },
        error: null,
        truth: {
          capability: "platform_impact.context_overview",
          level: "derived",
          profile: "production",
          freshness: { state: "fresh" },
        },
      }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Aghost"]}>
        <ExposurePathPage client={client} services={serviceOptions()} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("No proven ingress chain")).toBeInTheDocument();
    expect(screen.getByText("Public entrypoints")).toBeInTheDocument();
    expect(screen.getByText("WAF coverage")).toBeInTheDocument();
    expect(screen.getByText("TLS termination")).toBeInTheDocument();
    expect(screen.getByText("platform_impact.context_overview")).toBeInTheDocument();
  });
});
