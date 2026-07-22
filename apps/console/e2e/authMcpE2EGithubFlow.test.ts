import { describe, expect, it } from "vitest";

import { resolveMcpSetupCommand } from "./authMcpE2EGithubFlow.ts";

describe("MCP setup posture command", () => {
  it("uses the exact-source prebuilt CLI instead of rebuilding it with go run", () => {
    expect(
      resolveMcpSetupCommand("/tmp/exact-source-eshu", "/workspace/go", "http://127.0.0.1:29081"),
    ).toEqual({
      file: "/tmp/exact-source-eshu",
      args: ["mcp", "setup", "--hosted", "--service-url", "http://127.0.0.1:29081"],
    });
  });

  it("preserves the go-run fallback when no prebuilt CLI is supplied", () => {
    expect(resolveMcpSetupCommand("", "/workspace/go", "http://127.0.0.1:29081")).toEqual({
      file: "go",
      args: [
        "-C",
        "/workspace/go",
        "run",
        "./cmd/eshu",
        "mcp",
        "setup",
        "--hosted",
        "--service-url",
        "http://127.0.0.1:29081",
      ],
    });
  });
});
