// pages/tokens/TokenRevealPanel.test.tsx
// Regression guard for the codex P1: the reveal panel's MCP client config
// snippet must never point at the console's own origin
// (window.location.origin). jsdom in this suite runs at
// http://localhost:5174/ (see vite.config.ts test.environmentOptions), the
// same shape as the real bug report (console dev origin leaking into the
// snippet as a wrong-but-plausible MCP host). Without a configured
// mcpEndpointHost, the panel must render the unmistakable placeholder
// instead.
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TokenRevealPanel } from "./TokenRevealPanel";
import type { CreatedAPIToken } from "../../api/userProfile";

const token: CreatedAPIToken = {
  token_id: "tok-new",
  api_token: "raw-generated-token",
  issued_at: "2026-06-24T10:00:00Z",
};

// The config textarea has no associated <label> element (its heading is a
// plain <p>), so it is read by id rather than accessible name.
function configFieldValue(): string {
  const field = document.getElementById("token-reveal-config") as HTMLTextAreaElement | null;
  if (!field) {
    throw new Error("token-reveal-config field not found");
  }
  return field.value;
}

describe("TokenRevealPanel — MCP config snippet endpoint", () => {
  it("never emits the console's own origin as the MCP URL", () => {
    render(<TokenRevealPanel token={token} onDismiss={() => {}} />);
    expect(configFieldValue()).not.toContain(window.location.origin);
    expect(configFieldValue()).not.toContain("localhost:5174");
  });

  it("falls back to the unmistakable placeholder host when no endpoint is configured", () => {
    render(<TokenRevealPanel token={token} onDismiss={() => {}} />);
    expect(configFieldValue()).toContain("https://YOUR-ESHU-MCP-HOST/mcp/message");
  });

  it("uses an explicitly configured MCP endpoint host when supplied", () => {
    render(
      <TokenRevealPanel
        token={token}
        mcpEndpointHost="https://eshu.example.com"
        onDismiss={() => {}}
      />,
    );
    expect(configFieldValue()).toContain("https://eshu.example.com/mcp/message");
  });
});
