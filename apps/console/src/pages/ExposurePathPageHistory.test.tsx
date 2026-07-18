import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ExposurePathPage } from "./ExposurePathPage";
import { publicContext, serviceOptions } from "./ExposurePathPageTestFixtures";
import type { EshuApiClient } from "../api/client";

describe("ExposurePathPage history invalidation", () => {
  it("replaces stale canonical truth before resolving a new non-empty history selector", async () => {
    const get = vi.fn(async () => ({ data: publicContext(), error: null, truth: null }));
    const postJson = vi.fn(async () => ({
      count: 0,
      entities: [],
      limit: 10,
      truncated: false,
    }));
    const client = { get, postJson } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <ReplacementHistoryHarness client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Replace service parameter" }));

    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue(
      "missing-service",
    );
    expect(await screen.findByText(/No authorized service matches/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Trace ingress" }));
    expect(await screen.findByText(/No authorized service matches/)).toBeInTheDocument();
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(
      "/api/v0/services/workload%3Acheckout/context",
      expect.anything(),
    );
    expect(postJson).toHaveBeenCalledTimes(2);
  });

  it("clears stale truth for a new non-empty history selector while disconnected", async () => {
    const client = {
      get: vi.fn(async () => ({ data: publicContext(), error: null, truth: null })),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <DisconnectedReplacementHistoryHarness client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect and replace service" }));

    expect(
      await screen.findByText("Enter an internet-facing service to trace its ingress chain."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue(
      "missing-service",
    );
    expect(screen.getByRole("button", { name: "Trace ingress" })).toBeDisabled();
    expect(client.get).toHaveBeenCalledTimes(1);
  });

  it("clears stale truth when history removes the service while the API is disconnected", async () => {
    const client = {
      get: vi.fn(async () => ({ data: publicContext(), error: null, truth: null })),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/exposure?service=workload%3Acheckout"]}>
        <DisconnectedHistoryHarness client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Ingress chain")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect and remove service" }));

    expect(
      await screen.findByText("Enter an internet-facing service to trace its ingress chain."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Ingress chain")).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Service selection" })).toHaveValue("");
  });
});

function ReplacementHistoryHarness({
  client,
}: {
  readonly client: EshuApiClient;
}): React.JSX.Element {
  const navigate = useNavigate();
  return (
    <>
      <ExposurePathPage client={client} services={serviceOptions()} />
      <button onClick={() => navigate("/exposure?service=missing-service")} type="button">
        Replace service parameter
      </button>
    </>
  );
}

function DisconnectedReplacementHistoryHarness({
  client,
}: {
  readonly client: EshuApiClient;
}): React.JSX.Element {
  const navigate = useNavigate();
  const [activeClient, setActiveClient] = useState<EshuApiClient | undefined>(client);
  return (
    <>
      <ExposurePathPage client={activeClient} services={serviceOptions()} />
      <button
        onClick={() => {
          setActiveClient(undefined);
          navigate("/exposure?service=missing-service");
        }}
        type="button"
      >
        Disconnect and replace service
      </button>
    </>
  );
}

function DisconnectedHistoryHarness({
  client,
}: {
  readonly client: EshuApiClient;
}): React.JSX.Element {
  const navigate = useNavigate();
  const [activeClient, setActiveClient] = useState<EshuApiClient | undefined>(client);
  return (
    <>
      <ExposurePathPage client={activeClient} services={serviceOptions()} />
      <button
        onClick={() => {
          setActiveClient(undefined);
          navigate("/exposure");
        }}
        type="button"
      >
        Disconnect and remove service
      </button>
    </>
  );
}
