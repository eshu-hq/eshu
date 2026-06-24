import { render, screen } from "@testing-library/react";

import { GraphCanvas } from "./GraphCanvas";

describe("GraphCanvas", () => {
  it("uses an endpoint-neutral empty state for shared graph surfaces", () => {
    render(<GraphCanvas graph={{ nodes: [], edges: [] }} />);

    expect(screen.getByText("No graph rows returned from this source yet.")).toBeInTheDocument();
    expect(screen.queryByText(/code\/relationships/i)).not.toBeInTheDocument();
  });
});
