import { render, screen } from "@testing-library/react";
import { StatusStrip } from "./StatusStrip";

describe("StatusStrip", () => {
  it("makes demo mode and runtime truth visible", () => {
    render(
      <StatusStrip
        environment={{
          apiKey: "",
          apiBaseUrl: "",
          mode: "demo",
          recentApiBaseUrls: []
        }}
        runtime={{
          freshnessState: "fresh",
          health: "ready",
          profile: "local_full_stack"
        }}
      />
    );

    expect(screen.getByText("Demo fixtures")).toBeInTheDocument();
    expect(screen.getByText("ready")).toBeInTheDocument();
    expect(screen.getByText("local_full_stack")).toBeInTheDocument();
    expect(screen.getByText("fresh")).toBeInTheDocument();
  });
});
