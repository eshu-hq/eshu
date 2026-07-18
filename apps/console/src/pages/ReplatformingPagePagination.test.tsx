import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { ReplatformingPage } from "./ReplatformingPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("ReplatformingPage pagination", () => {
  it("pages from the submitted review when draft filters have changed", async () => {
    const calls: Record<string, unknown>[] = [];
    const client = {
      get: async () => selectorEnvelope(),
      post: async (path: string, body: Record<string, unknown>) => {
        calls.push(body);
        return {
          data: reviewSection(path),
          error: null,
          truth: truthEnvelope(),
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={["/replatforming?scope_kind=account&account_id=123456789012&offset=25"]}
      >
        <Routes>
          <Route
            element={
              <>
                <ReplatformingPage
                  client={client}
                  model={modelFromSnapshot(emptySnapshot("live"))}
                />
                <LocationProbe />
              </>
            }
            path="/replatforming"
          />
        </Routes>
      </MemoryRouter>,
    );

    const next = await screen.findByRole("button", { name: "Next page" });
    await waitFor(() => expect(next).toBeEnabled());
    fireEvent.change(screen.getByLabelText("Account"), { target: { value: "210987654321" } });
    fireEvent.click(next);

    expect(await screen.findByTestId("replatforming-location")).toHaveTextContent(
      "/replatforming?scope_kind=account&account_id=123456789012&offset=125",
    );
    await waitFor(() =>
      expect(calls.some((body) => body.account_id === "123456789012" && body.offset === 125)).toBe(
        true,
      ),
    );
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return (
    <output data-testid="replatforming-location">{location.pathname + location.search}</output>
  );
}

function selectorEnvelope() {
  return {
    data: {
      count: 2,
      empty_scope_count: 0,
      finding_kinds: [],
      limit: 200,
      page_sizes: [100],
      readiness: { detail: "ready", next_action: "Choose a scope.", state: "ready" },
      scopes: ["123456789012", "210987654321"].map((accountId) => ({
        account_id: accountId,
        finding_count: 1,
        label: `lambda in us-east-1 (account ...${accountId.slice(-4)})`,
        region: "us-east-1",
        scope_id: `aws:${accountId}:us-east-1:lambda`,
        service: "lambda",
      })),
      supported_scope_kinds: ["account", "region", "service"],
      truncated: false,
    },
    error: null,
    truth: truthEnvelope(),
  };
}

function reviewSection(path: string): Record<string, unknown> {
  if (path.endsWith("/plans")) {
    return {
      limit: 100,
      next_offset: 125,
      offset: 25,
      plan: { items: [], non_goals: [], waves: [] },
      story: "submitted review",
      truncated: true,
    };
  }
  if (path.endsWith("/rollups")) {
    return {
      dimensions: { account: [], environment: [], service: [] },
      story: "submitted review",
      truncated: false,
    };
  }
  return { ownership_packets: [], story: "submitted review", truncated: false };
}

function truthEnvelope() {
  return {
    basis: "semantic_facts",
    capability: "replatforming.plan.readiness",
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative",
  };
}
