import { render, screen, waitFor } from "@testing-library/react";
import type { EshuApiClient } from "../api/client";
import { ObservabilityPage } from "./ObservabilityPage";

describe("ObservabilityPage", () => {
  it("keeps provider empty state hidden while coverage is loading", () => {
    const client = {
      getJson: () => new Promise(() => {})
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    expect(screen.getByText("Loading observability coverage...")).toBeInTheDocument();
    expect(screen.queryByText(/No observability coverage/)).not.toBeInTheDocument();
  });

  it("labels empty coverage as empty rather than live", async () => {
    const client = {
      getJson: async () => ({ correlations: [], truncated: false })
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    await waitFor(() => expect(screen.getAllByText("empty").length).toBeGreaterThan(0));
    expect(screen.queryByText("live")).not.toBeInTheDocument();
  });
});
