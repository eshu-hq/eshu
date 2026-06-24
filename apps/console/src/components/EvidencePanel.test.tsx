import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { EvidencePanel, type EvidencePanelData } from "./EvidencePanel";

function dataFrom(overrides: Partial<EvidencePanelData> = {}): EvidencePanelData {
  return {
    kindLabel: "Edge evidence",
    title: "IMPORTS",
    truthLabel: "exact",
    truth: {
      basis: "authoritative_graph",
      capability: "visualization.derive",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "local_authoritative",
      reason: "graph projection"
    },
    facts: [
      { label: "From", value: "billing" },
      { label: "To", value: "payments" }
    ],
    sourceHref: "/repositories/up-1/source?path=go.mod&lineStart=12",
    sourceLabel: "go.mod:12",
    limitations: ["bounded subset"],
    ...overrides
  };
}

function renderPanel(data: EvidencePanelData, onClose = vi.fn()) {
  render(
    <MemoryRouter>
      <EvidencePanel data={data} onClose={onClose} />
    </MemoryRouter>
  );
  return onClose;
}

describe("EvidencePanel", () => {
  it("renders the title, kind label, and joined facts", () => {
    renderPanel(dataFrom());
    const panel = screen.getByRole("region", { name: /Evidence for IMPORTS/i });
    expect(within(panel).getByText("IMPORTS")).toBeInTheDocument();
    expect(within(panel).getByText("Edge evidence")).toBeInTheDocument();
    expect(within(panel).getByText("billing")).toBeInTheDocument();
    expect(within(panel).getByText("payments")).toBeInTheDocument();
  });

  it("maps the truth label to the console truth vocabulary as a chip", () => {
    // "exact" is a known truth label and renders as a colored chip, not raw text.
    renderPanel(dataFrom({ truthLabel: "exact" }));
    const panel = screen.getByRole("region");
    expect(within(panel).getAllByText("exact").length).toBeGreaterThan(0);
  });

  it("normalizes an API fallback truth label to the 'inferred' vocabulary", () => {
    renderPanel(dataFrom({ truthLabel: "fallback" }));
    expect(within(screen.getByRole("region")).getByText("inferred")).toBeInTheDocument();
  });

  it("renders an unknown truth label literally so uncertainty is never hidden", () => {
    renderPanel(dataFrom({ truthLabel: "ambiguous" }));
    expect(within(screen.getByRole("region")).getByText("ambiguous")).toBeInTheDocument();
  });

  it("renders packet truth basis, level, freshness, and reason", () => {
    renderPanel(dataFrom());
    const panel = screen.getByRole("region");
    expect(within(panel).getByText(/authoritative_graph/)).toBeInTheDocument();
    expect(within(panel).getByText("fresh")).toBeInTheDocument();
    expect(within(panel).getByText(/graph projection/)).toBeInTheDocument();
  });

  it("surfaces a stale freshness state without hiding it", () => {
    const data = dataFrom({
      truth: { capability: "c", freshness: { state: "stale" }, level: "derived", profile: "production" }
    });
    renderPanel(data);
    expect(within(screen.getByRole("region")).getByText("stale")).toBeInTheDocument();
  });

  it("renders a source link when a source href is provided", () => {
    renderPanel(dataFrom());
    expect(screen.getByRole("link", { name: /Open source/i })).toHaveAttribute(
      "href",
      "/repositories/up-1/source?path=go.mod&lineStart=12"
    );
  });

  it("stays explicit when truth, facts, and source are all absent", () => {
    renderPanel({ kindLabel: "Node evidence", title: "ghost", truthLabel: "", truth: null, facts: [] });
    const panel = screen.getByRole("region");
    expect(within(panel).getByText(/truth label not provided/i)).toBeInTheDocument();
    expect(within(panel).getByText(/packet truth unavailable/i)).toBeInTheDocument();
    expect(within(panel).queryByRole("link", { name: /Open source/i })).toBeNull();
  });

  it("omits empty fact rows rather than rendering blank label/value pairs", () => {
    renderPanel(dataFrom({ facts: [{ label: "From", value: "billing" }, { label: "To", value: "" }] }));
    const panel = screen.getByRole("region");
    expect(within(panel).getByText("billing")).toBeInTheDocument();
    expect(within(panel).queryByText("To")).toBeNull();
  });

  it("renders a custom section with its rows", () => {
    renderPanel(dataFrom({
      sections: [{ title: "Provenance", rows: [{ label: "Method", value: "import_scan" }] }]
    }));
    const panel = screen.getByRole("region");
    expect(within(panel).getByText("Provenance")).toBeInTheDocument();
    expect(within(panel).getByText("import_scan")).toBeInTheDocument();
  });

  it("renders evidence facts as a bulleted list section", () => {
    renderPanel(dataFrom({ evidence: ["import edge in go.mod", "lockfile pin"] }));
    const panel = screen.getByRole("region");
    expect(within(panel).getByText("import edge in go.mod")).toBeInTheDocument();
    expect(within(panel).getByText("lockfile pin")).toBeInTheDocument();
  });

  it("closes on the close button and on Escape", () => {
    const onClose = renderPanel(dataFrom());
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("is an inline region, not a modal dialog", () => {
    renderPanel(dataFrom());
    // The inline primitive must not trap the page behind a modal scrim.
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("region")).toBeInTheDocument();
  });
});
