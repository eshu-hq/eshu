import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { App } from "./App";

describe("Graph Explorer route", () => {
  afterEach(() => {
    window.localStorage.clear();
  });

  it("loads the code-split Explorer page", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({ mode: "demo", apiBaseUrl: "", recentApiBaseUrls: [] }),
    );

    render(
      <MemoryRouter initialEntries={["/explorer"]}>
        <App />
      </MemoryRouter>,
    );

    expect(
      await screen.findByRole("heading", { name: "Graph Explorer" }, { timeout: 4000 }),
    ).toBeInTheDocument();
  });
});
