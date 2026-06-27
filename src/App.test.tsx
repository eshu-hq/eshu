import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("renders the public marketing site with primary CTAs", () => {
    render(<App />);

    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: "The institutional knowledge layer now has an agentic answer surface.",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "What's new" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Ask Eshu" })).toBeInTheDocument();
    expect(screen.getByText(/Evidence packets v2/)).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Try it locally" })[0]).toHaveAttribute(
      "href",
      "#try-it",
    );
    expect(screen.getAllByRole("link", { name: "Read the docs" })[0]).toHaveAttribute(
      "href",
      "https://github.com/eshu-hq/eshu/tree/main/docs/public",
    );
    const displayLogo = screen.getByAltText("Eshu display logo");
    expect(displayLogo).toHaveAttribute("src", "/brand/eshu-social-preview-1200x630.png");
    expect(displayLogo.closest(".hero-logo-frame")).toHaveClass("hero-logo-frame--full-preview");
    expect(screen.getByLabelText("Source to runtime graph")).toBeInTheDocument();
    expect(screen.getByText(/eshu trace service checkout/)).toBeInTheDocument();
    expect(screen.getByText(/mcp: ask/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Where the graph shows up" })).toBeInTheDocument();
    expect(screen.getAllByText(/22. source languages/)).toHaveLength(2);
    expect(
      screen.getByRole("heading", { name: "Built for the whole organization" }),
    ).toBeInTheDocument();
    // vulnerability_intelligence renders in the always-visible proof card; its
    // other use is in the eshu-list command output, shown only when selected.
    expect(
      screen.getByText(/vulnerability_intelligence collector at promotion_state/),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "First prompts by role" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Read more" })).toBeInTheDocument();
  });

  it("lets visitors explore commands, personas, and cleanup modes", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Run the graph" })).toBeInTheDocument();
    expect(screen.getAllByText(/Graph ready for organization-wide questions/)).toHaveLength(2);

    expect(screen.queryByRole("button", { name: "eshu ask" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "POST /api/v0/ask" }));
    expect(
      screen.getByText(/Question: which services are affected by CVE-2024-3094/),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "eshu trace service checkout" }));
    expect(screen.getByText(/Service: checkout-service/)).toBeInTheDocument();
    expect(screen.getByText(/Trace status: partial/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Ask Eshu user" }));
    expect(screen.getByText("ask")).toBeInTheDocument();
    expect(
      screen.getByText(/returns evidence handles, truth class, missing evidence/),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Security analyst" }));
    expect(screen.getByText("list_supply_chain_impact_findings")).toBeInTheDocument();
    expect(
      screen.getByText(/Which of my workloads are affected by CVE-2025-13465/),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Unmanaged resources" }));
    expect(screen.getByText(/aws_s3_bucket.legacy-payment-logs/)).toBeInTheDocument();
  });
});
