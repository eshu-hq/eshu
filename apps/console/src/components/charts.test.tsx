import { render } from "@testing-library/react";

import { AreaChart, Sparkline } from "./charts";

// Regression: a single-datapoint series used to divide by (length - 1) === 0 and
// emit invalid SVG path commands (NaN/Infinity coordinates, empty `d`).
function pathData(container: HTMLElement): string {
  return Array.from(container.querySelectorAll("path"))
    .map((p) => p.getAttribute("d") ?? "")
    .join(" ");
}

describe("charts single-datapoint handling", () => {
  it("renders a Sparkline with one datapoint without invalid path coordinates", () => {
    const { container } = render(<Sparkline data={[42]} />);
    const d = pathData(container);
    expect(d.length).toBeGreaterThan(0);
    expect(d).not.toMatch(/NaN|Infinity/);
  });

  it("renders an AreaChart with one datapoint without invalid path coordinates", () => {
    const { container } = render(<AreaChart data={[42]} />);
    const d = pathData(container);
    expect(d.length).toBeGreaterThan(0);
    expect(d).not.toMatch(/NaN|Infinity/);
  });

  it("still shows the empty state for an empty AreaChart series", () => {
    const { getByText } = render(<AreaChart data={[]} />);
    expect(getByText("No series available from this source.")).toBeInTheDocument();
  });
});
