import { beforeEach, describe, expect, it, vi } from "vitest";

const mermaidMocks = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
}));

vi.mock("mermaid", () => ({
  default: {
    initialize: mermaidMocks.initialize,
    render: mermaidMocks.render,
  },
}));

describe("renderMermaid", () => {
  beforeEach(() => {
    vi.resetModules();
    mermaidMocks.initialize.mockReset();
    mermaidMocks.render.mockReset().mockResolvedValue({ svg: "<svg />" });
  });

  it("keeps the dark, strict rendering contract on Mermaid 12", async () => {
    const { renderMermaid } = await import("./mermaid");

    await renderMermaid("diagram-1", "flowchart LR\nA-->B");

    expect(mermaidMocks.initialize).toHaveBeenCalledWith({
      startOnLoad: false,
      theme: "dark",
      securityLevel: "strict",
      fontFamily: "JetBrains Mono, ui-monospace, monospace",
      layout: "dagre",
      look: "classic",
      themeVariables: {
        primaryColor: "#141d27",
        primaryBorderColor: "#2dd4bf",
        primaryTextColor: "#f3ebdd",
        lineColor: "#6b7682",
        secondaryColor: "#10171f",
        tertiaryColor: "#0c1219",
      },
    });
    expect(mermaidMocks.render).toHaveBeenCalledWith("diagram-1", "flowchart LR\nA-->B");
  });

  it("initializes only once while rendering multiple diagrams", async () => {
    const { renderMermaid } = await import("./mermaid");

    await renderMermaid("diagram-1", "flowchart LR\nA-->B");
    await renderMermaid("diagram-2", "flowchart TD\nC-->D");

    expect(mermaidMocks.initialize).toHaveBeenCalledTimes(1);
    expect(mermaidMocks.render).toHaveBeenCalledTimes(2);
  });
});
