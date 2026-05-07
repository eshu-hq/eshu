import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the public marketing site with primary CTAs", () => {
    render(<App />);

    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: "Find the true path through your stack."
      })
    ).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "View on GitHub" })[0]).toHaveAttribute(
      "href",
      "https://github.com/eshu-hq/eshu"
    );
    expect(screen.getAllByRole("link", { name: "Read the docs" })[0]).toHaveAttribute(
      "href",
      "https://github.com/eshu-hq/eshu/tree/main/docs/docs"
    );
    expect(screen.getByAltText("Eshu display logo")).toHaveAttribute(
      "src",
      "/brand/eshu-social-preview-1200x630.png"
    );
    expect(screen.getByLabelText("Source to runtime graph")).toBeInTheDocument();
    expect(screen.getByText(/eshu trace service checkout/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Where the graph shows up" })).toBeInTheDocument();
    expect(screen.getAllByText(/SQL, Terraform, Kubernetes/)).toHaveLength(3);
    expect(screen.getByRole("heading", { name: "Built for the whole organization" })).toBeInTheDocument();
    expect(screen.getAllByText(/nearly 900 repos/)).toHaveLength(2);
    expect(screen.getAllByText(/under 15 minutes/)).toHaveLength(2);
    expect(screen.getByRole("heading", { name: "Prompts for different jobs" })).toBeInTheDocument();
  });

  it("lets visitors explore commands, personas, and cleanup modes", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Run the graph" })).toBeInTheDocument();
    expect(screen.getAllByText(/Graph ready for organization-wide questions/)).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "eshu trace service checkout" }));
    expect(screen.getByText(/service: checkout-service/)).toBeInTheDocument();
    expect(screen.getByText(/k8s: namespace payments/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Leadership" }));
    expect(screen.getByText(/Nearly 900 repos indexed in under 15 minutes/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Dead IaC" }));
    expect(screen.getByText(/terraform\/modules\/legacy-cache/)).toBeInTheDocument();
  });
});
