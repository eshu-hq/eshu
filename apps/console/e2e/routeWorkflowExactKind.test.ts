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
  return responseBody({ candidate_kind: candidateKind, limit: 100 });
}

function responseBody(body: Record<string, unknown>): ExactKindResponseStub {
  return {
    request: () => ({
      method: () => "POST",
      postDataJSON: () => body,
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

  it("proves reset, count scope, repository drill-down, and observed language selection", async () => {
    const workflow = {
      ...exactKindWorkflow,
      deadCodeControls: {
        applyName: "Apply",
        breakdownLinkSelector: "#breakdown a",
        breakdownSelector: "#breakdown",
        breakdownToggleName: "Show repository breakdown",
        countScopeSelector: ".count-scope",
        expectedCountScopeText: "returned result window, not the corpus",
        languageOptionsSelector: "#languages option",
        languageSelector: "#language",
        observedLanguageSelector: ".observed-language",
        repositoryOptionsSelector: "#repositories option",
        repositorySelector: "#repository",
        resetKindName: "All kinds",
      },
    } as const;
    const selectedTrait = locatorStub({ getAttribute: vi.fn().mockResolvedValue("active") });
    const kindControls = locatorStub({
      allTextContents: vi.fn().mockResolvedValue(["All kinds", "trait · 22"]),
      nth: vi.fn(() => selectedTrait),
    });
    const cells = locatorStub({
      allTextContents: vi.fn().mockResolvedValue(["Trait"]),
      count: vi.fn().mockResolvedValue(1),
    });
    const reset = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const countScope = locatorStub({
      innerText: vi
        .fn()
        .mockResolvedValue(
          "100 candidate limit. All summary counts describe this returned result window, not the corpus.",
        ),
    });
    let pageUrl = "http://host/dead-code";
    const breakdownLink = locatorStub({
      click: vi.fn(async () => {
        pageUrl = "http://host/dead-code?repo_id=repository%3Ar1";
      }),
      getAttribute: vi.fn().mockResolvedValue("/dead-code?repo_id=repository%3Ar1"),
    });
    const repositoryInput = locatorStub({
      inputValue: vi.fn().mockResolvedValue("repository:r1"),
    });
    const observedLanguage = locatorStub({ innerText: vi.fn().mockResolvedValue("typescript") });
    const languageInput = locatorStub();
    const waitForResponse = vi
      .fn()
      .mockResolvedValueOnce(response("Trait"))
      .mockResolvedValueOnce(responseBody({ limit: 100 }))
      .mockResolvedValueOnce(responseBody({ limit: 100, repo_id: "repository:r1" }))
      .mockResolvedValueOnce(
        responseBody({ language: "typescript", limit: 100, repo_id: "repository:r1" }),
      );
    const page = {
      getByRole: vi.fn((_role: string, options: { readonly name: string }) => {
        if (options.name === "All kinds") return reset;
        return locatorStub();
      }),
      locator: vi.fn((selector: string) => {
        if (selector === `${workflow.groupSelector} button`) return kindControls;
        if (selector === workflow.outcomeCellSelector) return cells;
        if (selector === workflow.deadCodeControls.countScopeSelector) return countScope;
        if (selector === workflow.deadCodeControls.breakdownSelector) return locatorStub();
        if (selector === workflow.deadCodeControls.breakdownLinkSelector) return breakdownLink;
        if (selector.startsWith(workflow.deadCodeControls.repositoryOptionsSelector)) {
          return locatorStub();
        }
        if (selector === workflow.deadCodeControls.repositorySelector) return repositoryInput;
        if (selector === workflow.deadCodeControls.observedLanguageSelector) {
          return observedLanguage;
        }
        if (selector.startsWith(workflow.deadCodeControls.languageOptionsSelector)) {
          return locatorStub();
        }
        if (selector === workflow.deadCodeControls.languageSelector) return languageInput;
        if (selector === ".main") return locatorStub();
        return locatorStub();
      }),
      url: vi.fn(() => pageUrl),
      waitForResponse,
    } as unknown as Page;
    page.getByRole = vi.fn((_role: string, options: { readonly name: string }) => {
      if (options.name === "All kinds") return reset as never;
      if (options.name === "Show repository breakdown") return locatorStub() as never;
      if (options.name === "Apply") return locatorStub() as never;
      return locatorStub() as never;
    }) as never;

    const result = await executeRouteWorkflow(page, workflow, vi.fn());

    expect(result.passed).toBe(true);
    expect(result.detail).toContain("repository drill-down");
    expect(waitForResponse).toHaveBeenCalledTimes(4);
    expect(languageInput.fill).toHaveBeenCalledWith("typescript");
    expect(result.requests).toHaveLength(4);
  });
});
