import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub, type ResponseStub } from "./routeWorkflowProbesTestSupport";

const stateResponseExpectation = {
  path: "/api/v0/test-state",
  method: "GET" as const,
  acceptedStatuses: [200],
};
const stateNetwork = [
  {
    method: "GET",
    status: 200,
    url: "http://host/eshu-api/api/v0/test-state",
    failureText: null,
  },
] as const;

describe("executeRouteWorkflow", () => {
  it("rejects a state selector that exists but is hidden", async () => {
    const main = locatorStub();
    const hiddenState = locatorStub({ isVisible: vi.fn().mockResolvedValue(false) });
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : hiddenState)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "hidden-state",
        kind: "state",
        anySelectors: [".live-result"],
        requiredResponses: [stateResponseExpectation],
      },
      vi.fn(),
      stateNetwork,
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("visib");
  });

  it("accepts only an explicitly declared truthful empty state", async () => {
    const main = locatorStub();
    const noRows = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const exactEmpty = locatorStub({
      allTextContents: vi
        .fn()
        .mockResolvedValue(["No SBOM/attestation subjects from this source."]),
    });
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === ".sbom-row") return noRows;
        return exactEmpty;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "sbom-state",
        kind: "state",
        anySelectors: [".sbom-row"],
        requiredResponses: [stateResponseExpectation],
        emptyStates: [
          {
            selector: '[aria-label="SBOM evidence workbench"] td.empty',
            exactText: "No SBOM/attestation subjects from this source.",
          },
        ],
      },
      vi.fn(),
      stateNetwork,
    );

    expect(result.passed).toBe(true);
    expect(result.detail).toContain("truthful empty state");
  });

  it("rejects a near-match instead of treating any empty row as truthful", async () => {
    const main = locatorStub();
    const noRows = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const wrongEmpty = locatorStub({
      allTextContents: vi.fn().mockResolvedValue(["SBOM API failed unexpectedly."]),
    });
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === ".sbom-row") return noRows;
        return wrongEmpty;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "sbom-state",
        kind: "state",
        anySelectors: [".sbom-row"],
        requiredResponses: [stateResponseExpectation],
        emptyStates: [
          {
            selector: '[aria-label="SBOM evidence workbench"] td.empty',
            exactText: "No SBOM/attestation subjects from this source.",
          },
        ],
      },
      vi.fn(),
      stateNetwork,
    );

    expect(result.passed).toBe(false);
  });

  it.each([
    "Relationships unavailable: graph read failed",
    "Import cycle analysis unavailable: query timed out",
  ])("rejects a visible Code Graph error: %s", async (errorText) => {
    const main = locatorStub({ textContent: vi.fn().mockResolvedValue(errorText) });
    const canvas = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => (selector === ".main" ? main : canvas)),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "code-graph",
        kind: "state",
        anySelectors: [".gcanvas-svg"],
        requiredResponses: [stateResponseExpectation],
        forbiddenTexts: ["Relationships unavailable:", "Import cycle analysis unavailable:"],
      },
      vi.fn(),
      stateNetwork,
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("forbidden state");
  });

  it("fails the vulnerability workflow when the default reachable tab is broken", async () => {
    const main = locatorStub({
      textContent: vi.fn().mockResolvedValue("Vulnerabilities data unavailable"),
    });
    const reachableTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const catalogTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const brokenReachable = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const catalog = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === ".supply-chain-register-grid") return brokenReachable;
        return catalog;
      }),
      getByRole: vi.fn((_role: string, options: { name: string }) =>
        options.name === "Reachable in services" ? reachableTab : catalogTab,
      ),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "vulnerability-tabs",
        kind: "tabs",
        tabs: [
          {
            name: "Reachable in services",
            outcomeSelector: ".supply-chain-register-grid",
            forbiddenTexts: ["Vulnerabilities data unavailable"],
          },
          {
            name: "Known intelligence (catalog)",
            outcomeSelector: 'input[aria-label="Search advisories"]',
          },
        ],
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(catalogTab.click).not.toHaveBeenCalled();
  });

  it("proves both visible vulnerability tabs instead of only the catalog tab", async () => {
    const reachableTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const catalogTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const page = {
      locator: vi.fn(() => locatorStub()),
      getByRole: vi.fn((_role: string, options: { name: string }) =>
        options.name === "Reachable in services" ? reachableTab : catalogTab,
      ),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "vulnerability-tabs",
        kind: "tabs",
        tabs: [
          { name: "Reachable in services", outcomeSelector: ".supply-chain-register-grid" },
          {
            name: "Known intelligence (catalog)",
            outcomeSelector: 'input[aria-label="Search advisories"]',
          },
        ],
      },
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(reachableTab.click).toHaveBeenCalledOnce();
    expect(catalogTab.click).toHaveBeenCalledOnce();
    expect(result.dataShapes).toHaveLength(2);
  });

  it("proves retained service truth before independently exercising known intelligence", async () => {
    const reachableTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const catalogTab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const findingRow = locatorStub({
      getAttribute: vi.fn(async (name: string) =>
        name === "data-vulnerability-finding-id" ? "finding:one" : "[]",
      ),
    });
    const notProven = locatorStub();
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === "[data-vulnerability-finding-id]") return findingRow;
        if (selector === '[data-service-truth="not-proven"]') return notProven;
        return locatorStub();
      }),
      getByRole: vi.fn((_role: string, options: { name: string }) =>
        options.name === "Reachable in services" ? reachableTab : catalogTab,
      ),
    } as unknown as Page;
    const loadFindings = vi.fn(async (status: string) => ({
      data: {
        findings:
          status === "affected_exact"
            ? [{ finding_id: "finding:one", repository_id: "repository:r_1" }]
            : [],
      },
    }));

    const result = await executeRouteWorkflow(
      page,
      {
        id: "vulnerability-tabs",
        kind: "tabs",
        proveVulnerabilityServiceTruth: true,
        tabs: [
          { name: "Reachable in services", outcomeSelector: ".supply-chain-register-grid" },
          {
            name: "Known intelligence (catalog)",
            outcomeSelector: 'input[aria-label="Search advisories"]',
          },
        ],
      },
      vi.fn(),
      [],
      [],
      loadFindings,
    );

    expect(result.passed).toBe(true);
    expect(result.detail).toContain("service truth");
    expect(loadFindings).toHaveBeenCalledTimes(2);
    expect(catalogTab.click).toHaveBeenCalledOnce();
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

  it("fails closed when a retained-data environment anchor is missing", async () => {
    vi.stubEnv("ESHU_E2E_MISSING_RETAINED_ANCHOR", "");
    const input = locatorStub();
    const page = {
      locator: vi.fn(() => input),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "retained-anchor",
        kind: "submit",
        fields: [
          {
            selector: 'input[aria-label="Incident id"]',
            valueEnv: "ESHU_E2E_MISSING_RETAINED_ANCHOR",
          },
        ],
        role: "button",
        name: "Review incident",
        expectedRequestPath: "/api/v0/incidents/${ESHU_E2E_MISSING_RETAINED_ANCHOR}/context",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".incident-summary",
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("ESHU_E2E_MISSING_RETAINED_ANCHOR");
    expect(input.fill).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
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
    const response: ResponseStub = {
      request: () => ({ method: () => "POST" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/search/semantic",
    };
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
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".sem-result-announce",
      },
      waitForQuiet,
    );

    expect(result.passed).toBe(true);
    expect(repository.fill).toHaveBeenCalledWith("repository:probe");
    expect(query.fill).toHaveBeenCalledWith("bounded probe");
    expect(submit.waitFor).toHaveBeenCalledWith({ state: "visible", timeout: 10_000 });
    expect(submit.click).toHaveBeenCalledOnce();
    expect(waitForQuiet).toHaveBeenCalledOnce();
  });

  it("scopes a submit button to its owning form when the page has duplicate names", async () => {
    const repository = locatorStub({ inputValue: vi.fn().mockResolvedValue("local") });
    const query = locatorStub({ inputValue: vi.fn().mockResolvedValue("deployment entrypoints") });
    const outcome = locatorStub();
    const scopedSubmit = locatorStub();
    const globalSubmit = locatorStub({ count: vi.fn().mockResolvedValue(2) });
    const form = locatorStub({ getByRole: vi.fn(() => scopedSubmit) });
    const response: ResponseStub = {
      request: () => ({ method: () => "POST" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/search/semantic",
    };
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".semantic-search-form") return form;
        if (selector === "#repo") return repository;
        if (selector === "#query") return query;
        return outcome;
      }),
      getByRole: vi.fn(() => globalSubmit),
      waitForResponse: vi.fn(async () => response),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "semantic-submit",
        kind: "submit",
        fields: [
          { selector: "#repo", value: "local" },
          { selector: "#query", value: "deployment entrypoints" },
        ],
        role: "button",
        name: "Search",
        scopeSelector: ".semantic-search-form",
        expectedRequestPath: "/api/v0/search/semantic",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".sem-result-announce",
      } as never,
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(form.getByRole).toHaveBeenCalled();
    expect(page.getByRole).not.toHaveBeenCalled();
  });
});
