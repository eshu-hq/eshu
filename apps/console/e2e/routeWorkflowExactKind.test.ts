import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub } from "./routeWorkflowProbesTestSupport";

interface ExactKindResponseStub {
  readonly request: () => {
    readonly method: () => string;
    readonly postDataJSON: () => unknown;
  };
  readonly status: () => number;
  readonly url: () => string;
}

const exactKindWorkflow = {
  acceptedResponseStatuses: [200],
  expectedRequestMethod: "POST",
  expectedRequestPath: "/api/v0/code/dead-code",
  groupSelector: '[aria-label="Dead-code kind filter"]',
  id: "dead-code-kind",
  kind: "exactKind",
  outcomeCellSelector: ".evidence-workbench tbody tr.cloud-row td:nth-child(2)",
  preferredName: "Trait",
} as const;

function response(candidateKind: string): ExactKindResponseStub {
  return {
    request: () => ({
      method: () => "POST",
      postDataJSON: () => ({ candidate_kind: candidateKind, limit: 100 }),
    }),
    status: () => 200,
    url: () => "http://host/eshu-api/api/v0/code/dead-code",
  };
}

function exactKindPage(waitForResponse: ReturnType<typeof vi.fn>): Page {
  const selected = locatorStub({ getAttribute: vi.fn().mockResolvedValue("active") });
  const controls = locatorStub({
    // The live control renders the API's display label in lower case while the
    // request contract requires the canonical candidate kind `Trait`.
    allTextContents: vi.fn().mockResolvedValue(["All kinds", "trait · 22"]),
    nth: vi.fn(() => selected),
  });
  const cells = locatorStub({
    allTextContents: vi.fn().mockResolvedValue(["Trait", "Trait"]),
    count: vi.fn().mockResolvedValue(2),
  });
  const main = locatorStub();
  return {
    locator: vi.fn((selector: string) => {
      if (selector === `${exactKindWorkflow.groupSelector} button`) return controls;
      if (selector === exactKindWorkflow.outcomeCellSelector) return cells;
      if (selector === ".main") return main;
      return locatorStub();
    }),
    waitForResponse,
  } as unknown as Page;
}

describe("exact-kind route workflow", () => {
  it("rejects visible exact-kind cells when no narrowed request is observed", async () => {
    const waitForResponse = vi.fn().mockRejectedValue(new Error("no matching response"));

    const result = await executeRouteWorkflow(
      exactKindPage(waitForResponse),
      exactKindWorkflow as never,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("no matching response");
    expect(waitForResponse).toHaveBeenCalledOnce();
  });

  it("rejects visible Trait cells when the request body asks for another kind", async () => {
    const waitForResponse = vi.fn().mockResolvedValue(response("Function"));

    const result = await executeRouteWorkflow(
      exactKindPage(waitForResponse),
      exactKindWorkflow as never,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("candidate_kind");
  });

  it("rejects a successful response whose request body is not valid JSON", async () => {
    const malformed = response("Trait");
    const waitForResponse = vi.fn().mockResolvedValue({
      ...malformed,
      request: () => ({
        method: () => "POST",
        postDataJSON: () => {
          throw new SyntaxError("invalid JSON");
        },
      }),
    });

    const result = await executeRouteWorkflow(
      exactKindPage(waitForResponse),
      exactKindWorkflow,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("candidate_kind missing or invalid");
  });

  it("records the exact narrowed request with the successful Trait proof", async () => {
    const waitForResponse = vi.fn().mockResolvedValue(response("Trait"));

    const result = await executeRouteWorkflow(
      exactKindPage(waitForResponse),
      exactKindWorkflow,
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(result.requests).toEqual([
      { method: "POST", pathname: "/eshu-api/api/v0/code/dead-code", status: 200 },
    ]);
  });
});
