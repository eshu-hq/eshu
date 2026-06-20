// mermaid.ts — lazy, code-split Mermaid renderer for diagram artifacts.
//
// Mermaid is heavy, so it is imported dynamically: the chunk loads only when a
// user actually views a diagram artifact, keeping it out of the main console
// bundle. securityLevel is "strict" so diagram labels are sanitized and click
// bindings are disabled — answer-supplied diagram source cannot inject markup
// or handlers. Callers must handle a rejected promise by falling back to the
// diagram source.
let initialized = false;

// renderMermaid renders Mermaid source to an SVG string. The id must be unique
// per render call (Mermaid uses it for internal element ids).
export async function renderMermaid(id: string, source: string): Promise<string> {
  const mermaid = (await import("mermaid")).default;
  if (!initialized) {
    mermaid.initialize({
      startOnLoad: false,
      theme: "dark",
      securityLevel: "strict",
      fontFamily: "JetBrains Mono, ui-monospace, monospace",
      themeVariables: {
        primaryColor: "#141d27",
        primaryBorderColor: "#2dd4bf",
        primaryTextColor: "#f3ebdd",
        lineColor: "#6b7682",
        secondaryColor: "#10171f",
        tertiaryColor: "#0c1219"
      }
    });
    initialized = true;
  }
  const { svg } = await mermaid.render(id, source);
  return svg;
}
