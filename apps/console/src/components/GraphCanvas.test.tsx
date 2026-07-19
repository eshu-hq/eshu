import { act, fireEvent, render, screen } from "@testing-library/react";

import { GraphCanvas } from "./GraphCanvas";

describe("GraphCanvas", () => {
  it("uses an endpoint-neutral empty state for shared graph surfaces", () => {
    render(<GraphCanvas graph={{ nodes: [], edges: [] }} />);

    expect(screen.getByText("No graph rows returned from this source yet.")).toBeInTheDocument();
    expect(screen.queryByText(/code\/relationships/i)).not.toBeInTheDocument();
  });

  it("expands a seven-lane layered graph so node boxes do not overlap horizontally", () => {
    render(
      <GraphCanvas
        graph={{
          edges: [],
          nodes: Array.from({ length: 7 }, (_, col) => ({
            col,
            id: `node:${col}`,
            kind: "service",
            label: `node-${col}`,
          })),
        }}
      />,
    );

    expect(document.querySelector(".gcanvas-svg")).toHaveAttribute("viewBox", "0 0 1400 640");
  });

  it("expands a dense layered lane so node boxes do not overlap vertically", () => {
    render(
      <GraphCanvas
        graph={{
          edges: [],
          nodes: Array.from({ length: 8 }, (_, index) => ({
            col: 0,
            id: `node:${index}`,
            kind: "instance",
            label: `node-${index}`,
          })),
        }}
      />,
    );

    expect(document.querySelector(".gcanvas-svg")).toHaveAttribute("viewBox", "0 0 1080 648");
  });

  it("exposes selectable graph nodes to keyboard and assistive technology", () => {
    const onSelect = vi.fn();
    render(
      <GraphCanvas
        graph={{
          edges: [],
          nodes: [
            {
              col: 0,
              id: "instance:catalog:prod",
              kind: "instance",
              label: "prod",
              sub: "runtime instance",
            },
          ],
        }}
        onSelect={onSelect}
      />,
    );

    const node = screen.getByRole("button", { name: "prod — runtime instance" });
    act(() => node.focus());
    fireEvent.keyDown(node, { key: "Enter" });
    fireEvent.keyDown(node, { key: " " });

    expect(node).toHaveFocus();
    expect(onSelect).toHaveBeenCalledTimes(2);
  });
});
