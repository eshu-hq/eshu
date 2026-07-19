// pages/tokens/TokenRevealPanel.tsx
// Reveal-once panel for a freshly created or rotated API token (issue
// #5164). Mirrors the reveal-once pattern SetupMFAStep.tsx established for
// recovery codes: show the secret with a copy affordance, require an
// explicit "I've saved this" acknowledgement before the caller can dismiss,
// and never make the raw value re-fetchable. Also renders a ready-to-paste
// MCP client config snippet that references ${ESHU_API_KEY} instead of
// embedding the token (mcpConfigSnippet.ts) — the console never writes the
// raw token into that snippet.
//
// The MCP endpoint host must never default to window.location.origin: the
// console UI and the MCP server (/mcp/message) are separate endpoints (the
// Vite dev proxy only forwards /eshu-api, and production is not guaranteed
// to route MCP traffic through the console's own origin either), so that
// default silently produces a concrete-but-wrong URL. There is currently no
// console runtime config that carries a real configured MCP endpoint, so
// mcpEndpointHost defaults to blank and buildMcpConfigSnippet renders its
// own unmistakable fill-in placeholder instead.
import { Check, Copy, ShieldAlert } from "lucide-react";
import { useState } from "react";

import type { CreatedAPIToken } from "../../api/userProfile";
import { buildMcpConfigSnippet, mcpApiKeyEnvVar } from "../../lib/mcpConfigSnippet";

import "./apiTokenControls.css";

async function copyToClipboard(value: string, mark: (copied: boolean) => void): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    mark(true);
    setTimeout(() => mark(false), 1600);
  } catch {
    // Clipboard access can be denied; the value remains selectable/readable.
  }
}

export function TokenRevealPanel({
  token,
  mcpEndpointHost,
  onDismiss,
}: {
  readonly token: CreatedAPIToken;
  // mcpEndpointHost is the actual configured MCP endpoint host, when one is
  // known (tests pass a fixed value to make assertions deterministic).
  // Production callers currently have no configured MCP endpoint to pass, so
  // this is omitted and buildMcpConfigSnippet falls back to its placeholder
  // host — it must NEVER be filled in with the console's own origin
  // (window.location.origin), because the console and the MCP server are
  // separate endpoints.
  readonly mcpEndpointHost?: string;
  readonly onDismiss: () => void;
}): React.JSX.Element {
  const [ack, setAck] = useState(false);
  const [copiedToken, setCopiedToken] = useState(false);
  const [copiedConfig, setCopiedConfig] = useState(false);
  const configSnippet = buildMcpConfigSnippet(mcpEndpointHost ?? "");

  return (
    <div className="token-reveal" role="region" aria-label="New API token">
      <div className="token-reveal-head">
        <ShieldAlert aria-hidden />
        <span>
          <strong>This is the only time the raw token is shown.</strong> Copy it now and store it
          somewhere safe — reloading this page will not show it again.
        </span>
      </div>

      <label htmlFor="token-reveal-value">API token</label>
      <div className="token-reveal-copy-row">
        <input
          id="token-reveal-value"
          type="text"
          readOnly
          value={token.api_token}
          onFocus={(e) => e.currentTarget.select()}
        />
        <button
          type="button"
          className="btn-ghost"
          onClick={() => void copyToClipboard(token.api_token, setCopiedToken)}
        >
          {copiedToken ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
          {copiedToken ? "Copied" : "Copy"}
        </button>
      </div>

      <p className="section-label">MCP client config</p>
      <p className="token-reveal-hint">
        Paste into your MCP client config, then export <code>{mcpApiKeyEnvVar}</code> with the token
        above before launching the client. The config below never embeds the raw token.
      </p>
      <div className="token-reveal-copy-row">
        <textarea id="token-reveal-config" readOnly rows={8} value={configSnippet} />
        <button
          type="button"
          className="btn-ghost"
          onClick={() => void copyToClipboard(configSnippet, setCopiedConfig)}
        >
          {copiedConfig ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
          {copiedConfig ? "Copied" : "Copy"}
        </button>
      </div>

      <label className="token-reveal-ack" htmlFor="token-reveal-ack">
        <input
          id="token-reveal-ack"
          type="checkbox"
          checked={ack}
          onChange={(e) => setAck(e.target.checked)}
        />
        <span>I&apos;ve saved this token and understand it won&apos;t be shown again.</span>
      </label>

      <button type="button" className="token-reveal-done" disabled={!ack} onClick={onDismiss}>
        Done
      </button>
    </div>
  );
}
