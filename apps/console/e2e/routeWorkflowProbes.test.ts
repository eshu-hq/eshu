import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { executeRouteWorkflow } from "./routeWorkflowProbes";

interface LocatorStub {
  getAttribute: ReturnType<typeof vi.fn>;
  count: ReturnType<typeof vi.fn>;
  click: ReturnType<typeof vi.fn>;
  fill: ReturnType<typeof vi.fn>;
  inputValue: ReturnType<typeof vi.fn>;
  textContent: ReturnType<typeof vi.fn>;
}

interface ResponseStub {
  url: () => string;
}

function locatorStub(overrides: Partial<LocatorStub> = {}): LocatorStub {
  return {
    count: vi.fn().mockResolvedValue(1),
    getAttribute: vi.fn().mockResolvedValue(null),
    click: vi.fn().mockResolvedValue(undefined),
    fill: vi.fn().mockResolvedValue(undefined),
    inputValue: vi.fn().mockResolvedValue(""),
    textContent: vi.fn().mockResolvedValue("live route content"),
    ...overrides,
  };
}

describe("executeRouteWorkflow", () => {
  it("passes a state workflow only when a required live selector is present", async () => {
    const main = locatorStub();
    const liveState = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : liveState)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      { id: "status-live", kind: "state", anySelectors: [".status-hero"] },
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(liveState.count).toHaveBeenCalledOnce();
  });

  it("fills a real control, waits for API quiet, and verifies the entered value", async () => {
    const main = locatorStub();
    const input = locatorStub({ inputValue: vi.fn().mockResolvedValue("repository") });
    const outcome = locatorStub({
      textContent: vi
        .fn()
        .mockResolvedValueOnce("three candidate rows")
        .mockResolvedValueOnce("one matching repository row"),
    });
    const waitForQuiet = vi.fn().mockResolvedValue(undefined);
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === ".results") return outcome;
        return input;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "nodes-filter",
        kind: "fill",
        selector: "input[aria-label='Find a node']",
        value: "repository",
        outcomeSelector: ".results",
        requireOutcomeChange: true,
      },
      waitForQuiet,
    );

    expect(input.fill).toHaveBeenCalledWith("repository");
    expect(waitForQuiet).toHaveBeenCalledOnce();
    expect(result.passed).toBe(true);
  });

  it("fails a fill workflow when only the input changes", async () => {
    const main = locatorStub();
    const input = locatorStub({ inputValue: vi.fn().mockResolvedValue("repository") });
    const outcome = locatorStub({ textContent: vi.fn().mockResolvedValue("unchanged rows") });
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === ".results") return outcome;
        return input;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "dead-code-filter",
        kind: "fill",
        selector: "input",
        value: "repository",
        outcomeSelector: ".results",
        requireOutcomeChange: true,
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("did not change");
  });

  it("requires the exact configured outcome text for a fill proof", async () => {
    const input = locatorStub({ inputValue: vi.fn().mockResolvedValue("Trait") });
    const outcome = locatorStub({ textContent: vi.fn().mockResolvedValue("Class candidates") });
    const page = {
      locator: vi.fn((selector: string) => (selector === ".results" ? outcome : input)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "dead-code-trait",
        kind: "fill",
        selector: "input",
        value: "Trait",
        outcomeSelector: ".results",
        outcomeTextIncludes: "Trait",
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("Trait");
  });

  it("submits a bounded live request and requires its result surface", async () => {
    const repository = locatorStub({ inputValue: vi.fn().mockResolvedValue("repository:probe") });
    const query = locatorStub({ inputValue: vi.fn().mockResolvedValue("bounded probe") });
    const outcome = locatorStub();
    const submit = locatorStub();
    const response: ResponseStub = { url: () => "http://host/eshu-api/api/v0/search/semantic" };
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === "#repo") return repository;
        if (selector === "#query") return query;
        return outcome;
      }),
      getByRole: vi.fn(() => submit),
      waitForResponse: vi.fn(async (predicate: (candidate: ResponseStub) => boolean) => {
        expect(predicate(response)).toBe(true);
        return response;
      }),
    } as unknown as Page;
    const waitForQuiet = vi.fn().mockResolvedValue(undefined);

    const result = await executeRouteWorkflow(
      page,
      {
        id: "semantic-submit",
        kind: "submit",
        fields: [
          { selector: "#repo", value: "repository:probe" },
          { selector: "#query", value: "bounded probe" },
        ],
        role: "button",
        name: "Search",
        expectedRequestPath: "/api/v0/search/semantic",
        outcomeSelector: ".sem-result-announce",
      },
      waitForQuiet,
    );

    expect(result.passed).toBe(true);
    expect(repository.fill).toHaveBeenCalledWith("repository:probe");
    expect(query.fill).toHaveBeenCalledWith("bounded probe");
    expect(submit.click).toHaveBeenCalledOnce();
    expect(waitForQuiet).toHaveBeenCalledOnce();
  });

  it("clicks a named tab and requires its route-specific outcome", async () => {
    const main = locatorStub();
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const tabPanel = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : tabPanel)),
      getByRole: vi.fn(() => tab),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
      },
      vi.fn(),
    );

    expect(tab.click).toHaveBeenCalledOnce();
    expect(tab.getAttribute).toHaveBeenCalledWith("aria-selected");
    expect(result.passed).toBe(true);
  });

  it("fails a click workflow when the named tab never becomes selected", async () => {
    const main = locatorStub();
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("false") });
    const page = {
      locator: vi.fn(() => main),
      getByRole: vi.fn(() => tab),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("aria-selected");
  });
});
