// Unit tests for the Vite readiness parser (issue #4971). The colorized-output
// case is the exact CI-only regression that hung the auth-e2e job for 30 min:
// under GitHub Actions Vite wraps the "Local:" port in ANSI escapes, which the
// naive regex could not match, so the readiness wait timed out even though the
// server was up.
import { describe, expect, it } from "vitest";

import { parseViteLocalUrl } from "./authE2EDevServer.ts";

const ESC = "\u001b";

describe("parseViteLocalUrl", () => {
  it("parses the plain (non-colorized) Local line", () => {
    expect(parseViteLocalUrl("  ➔  Local:   http://127.0.0.1:5185/\n")).toBe(
      "http://127.0.0.1:5185",
    );
  });

  it("parses the ANSI-colorized Local line CI emits (port wrapped in escapes)", () => {
    // Mirrors GitHub Actions output: the port is wrapped in ANSI bold escapes,
    // e.g. `http://127.0.0.1:<ESC>[1m5185<ESC>[22m/`.
    const colorized = `  ${ESC}[32m➔${ESC}[39m  ${ESC}[1mLocal${ESC}[22m:   ${ESC}[36mhttp://127.0.0.1:${ESC}[1m5185${ESC}[22m/${ESC}[39m\n`;
    expect(parseViteLocalUrl(colorized)).toBe("http://127.0.0.1:5185");
  });

  it("returns null for a chunk without a Local URL", () => {
    expect(parseViteLocalUrl("VITE v7.3.5  ready in 264 ms\n")).toBeNull();
    expect(parseViteLocalUrl("")).toBeNull();
  });
});
