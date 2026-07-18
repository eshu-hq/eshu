import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { ExposurePathPage } from "./ExposurePathPage";
import { publicContext, serviceOptions } from "./ExposurePathPageTestFixtures";
import type { EshuApiClient } from "../api/client";

describe("ExposurePathPage history invalidation", () => {
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
