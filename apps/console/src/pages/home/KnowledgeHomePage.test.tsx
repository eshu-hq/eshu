import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { KnowledgeHomePage } from "./KnowledgeHomePage";
import { EshuApiClient } from "../../api/client";
import { createDemoApiClient } from "../../api/demoClient";

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="location">{location.pathname + location.search}</output>;
}

function renderHome(client: EshuApiClient, onFind = vi.fn()): ReturnType<typeof render> {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route path="/" element={<KnowledgeHomePage client={client} onFind={onFind} />} />
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("KnowledgeHomePage", () => {
  it("renders every panel from the contract-validated demo fixtures", async () => {
    renderHome(createDemoApiClient());

    expect(await screen.findByText(/connected to 128 repositories, 64 services/)).toBeInTheDocument();
    expect(await screen.findByText("Knowledge is up to date.")).toBeInTheDocument();
    expect(await screen.findByText("6 urgent vulnerabilities affect software we run")).toBeInTheDocument();
    expect(await screen.findByText("team-payments")).toBeInTheDocument();
    expect(await screen.findByText("Guided vulnerable-dependency investigation")).toBeInTheDocument();
    const coverage = screen.getByRole("region", { name: "What Eshu can see" });
    expect(within(coverage).getByText("Code & ownership")).toBeInTheDocument();
  });

  it("sends questions to Ask and names to search", async () => {
    const onFind = vi.fn();
    renderHome(createDemoApiClient(), onFind);
    const box = screen.getByRole("textbox", { name: "Search or ask Eshu" });

    fireEvent.change(box, { target: { value: "payments-api" } });
    fireEvent.submit(box.closest("form") as HTMLFormElement);
    expect(onFind).toHaveBeenCalledWith("payments-api");

    fireEvent.change(box, { target: { value: "who owns payments-api?" } });
    fireEvent.submit(box.closest("form") as HTMLFormElement);
    await waitFor(() =>
      expect(screen.getByTestId("location").textContent).toBe("/ask?q=who%20owns%20payments-api%3F"),
    );
  });

  it("swaps suggested questions when the role lens changes", async () => {
    renderHome(createDemoApiClient());
    fireEvent.click(await screen.findByRole("radio", { name: "Security" }));
    expect(
      await screen.findByRole("button", { name: "Which services are affected by CVE-2025-13465?" }),
    ).toBeInTheDocument();
  });

  it("degrades one panel at a time when reads fail", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString();
      if (url.includes("/supply-chain/")) return new Response("upstream request timeout", { status: 504 });
      if (url.includes("/status/collector-readiness")) return new Response("{}", { status: 403 });
      return createDemoFetch(url);
    });
    renderHome(new EshuApiClient({ baseUrl: "http://eshu.test/", fetcher, timeoutMs: 0 }));

    expect(await screen.findByText("Vulnerability status couldn't be loaded")).toBeInTheDocument();
    const coverage = screen.getByRole("region", { name: "What Eshu can see" });
    expect(await within(coverage).findByText(/doesn't have access/)).toBeInTheDocument();
    expect(await screen.findByText("team-payments")).toBeInTheDocument();
  });
});

const demo = createDemoApiClient();
async function createDemoFetch(url: string): Promise<Response> {
  const path = new URL(url).pathname + new URL(url).search;
  const envelope = await demo.get<unknown>(path);
  return Response.json(envelope);
}
