import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { repositoryApiPathFromSourcePath } from "./repositoryRouteWorkflowProbe";
import { executeRouteWorkflow, repositoryPathsFromSourceHref } from "./routeWorkflowProbes";

interface LocatorStub {
  allTextContents: ReturnType<typeof vi.fn>;
  getAttribute: ReturnType<typeof vi.fn>;
  count: ReturnType<typeof vi.fn>;
  click: ReturnType<typeof vi.fn>;
  fill: ReturnType<typeof vi.fn>;
  inputValue: ReturnType<typeof vi.fn>;
  isVisible: ReturnType<typeof vi.fn>;
  nth: ReturnType<typeof vi.fn>;
  textContent: ReturnType<typeof vi.fn>;
  waitFor: ReturnType<typeof vi.fn>;
}

interface ResponseStub {
  request: () => { method: () => string; postDataJSON?: () => unknown };
  status: () => number;
  url: () => string;
}

function locatorStub(overrides: Partial<LocatorStub> = {}): LocatorStub {
  const stub: LocatorStub = {
    allTextContents: vi.fn().mockResolvedValue([]),
    count: vi.fn().mockResolvedValue(1),
    getAttribute: vi.fn().mockResolvedValue(null),
    click: vi.fn().mockResolvedValue(undefined),
    fill: vi.fn().mockResolvedValue(undefined),
    inputValue: vi.fn().mockResolvedValue(""),
    isVisible: vi.fn().mockResolvedValue(true),
    nth: vi.fn(),
    textContent: vi.fn().mockResolvedValue("live route content"),
    waitFor: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  if (overrides.nth === undefined) stub.nth.mockImplementation(() => stub);
  return stub;
}

describe("executeRouteWorkflow interactions", () => {
  it("fails a submit workflow when its typed anchor is absent from the request body", async () => {
    const field = locatorStub({ inputValue: vi.fn().mockResolvedValue("repository:probe") });
    const outcome = locatorStub();
    const submit = locatorStub();
    const response: ResponseStub = {
      request: () => ({
        method: () => "POST",
        postDataJSON: () => ({ query: "bounded probe" }),
      }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/search/semantic",
    };
    const page = {
      locator: vi.fn((selector: string) => (selector === "#repo" ? field : outcome)),
      getByRole: vi.fn(() => submit),
      waitForResponse: vi.fn(async () => response),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "semantic-request-anchor",
        kind: "submit",
        fields: [{ selector: "#repo", value: "repository:probe", requestKey: "repo_id" }],
        role: "button",
        name: "Search",
        expectedRequestPath: "/api/v0/search/semantic",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".sem-result-announce",
      } as never,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("repo_id");
  });

  it("fails a fill workflow when its request query changes the typed value", async () => {
    const input = locatorStub({ inputValue: vi.fn().mockResolvedValue("repository") });
    const outcome = locatorStub();
    const response: ResponseStub = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/graph/entities?q=service",
    };
    const page = {
      locator: vi.fn((selector: string) => (selector === "#query" ? input : outcome)),
      waitForResponse: vi.fn(async () => response),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "nodes-request-anchor",
        kind: "fill",
        selector: "#query",
        value: "repository",
        requestKey: "q",
        outcomeSelector: ".results",
        expectedRequestPath: "/api/v0/graph/entities",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
      } as never,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("q");
  });

  it("rejects a response with the right path but the wrong HTTP method", async () => {
    const field = locatorStub({ inputValue: vi.fn().mockResolvedValue("local") });
    const outcome = locatorStub();
    const submit = locatorStub();
    const wrongMethod = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/search/semantic",
    };
    const page = {
      locator: vi.fn((selector: string) => (selector === "#repo" ? field : outcome)),
      getByRole: vi.fn(() => submit),
      waitForResponse: vi.fn(async (predicate: (candidate: typeof wrongMethod) => boolean) => {
        if (!predicate(wrongMethod)) throw new Error("no matching response");
        return wrongMethod;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "semantic-method-contract",
        kind: "submit",
        fields: [{ selector: "#repo", value: "local" }],
        role: "button",
        name: "Search",
        expectedRequestPath: "/api/v0/search/semantic",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".sem-result-announce",
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("no matching response");
  });

  it("fails closed when the required retained exact kind is absent", async () => {
    const selected = locatorStub({ getAttribute: vi.fn().mockResolvedValue("active") });
    const controls = locatorStub({
      allTextContents: vi.fn().mockResolvedValue(["All kinds", "function · 100"]),
      nth: vi.fn(() => selected),
    });
    const cells = locatorStub({
      count: vi.fn().mockResolvedValue(2),
      allTextContents: vi.fn().mockResolvedValue(["function", "function"]),
    });
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === '[aria-label="Dead-code kind filter"] button') return controls;
        if (selector === ".evidence-workbench tbody tr.cloud-row td:nth-child(2)") return cells;
        return locatorStub();
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "dead-code-kind",
        kind: "exactKind",
        groupSelector: '[aria-label="Dead-code kind filter"]',
        preferredName: "Trait",
        outcomeCellSelector: ".evidence-workbench tbody tr.cloud-row td:nth-child(2)",
      } as never,
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required exact kind Trait was absent");
    expect(selected.click).not.toHaveBeenCalled();
  });

  it("clicks a named tab and requires its route-specific outcome", async () => {
    const main = locatorStub();
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const tabPanel = locatorStub();
    const loadedPolicy = locatorStub();
    const response: ResponseStub = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/auth/admin/sign-in-policy",
    };
    const page = {
      locator: vi.fn((selector: string) => {
        if (selector === ".main") return main;
        if (selector === "#policy-require-sso") return loadedPolicy;
        return tabPanel;
      }),
      getByRole: vi.fn(() => tab),
      waitForResponse: vi.fn(async (predicate: (candidate: ResponseStub) => boolean) => {
        if (!predicate(response)) throw new Error("no matching response");
        return response;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
        loadedStateSelector: "#policy-require-sso",
        expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
        forbiddenText: "Sign-in policy unavailable from this source.",
      },
      vi.fn(),
    );

    expect(tab.click).toHaveBeenCalledOnce();
    expect(tab.getAttribute).toHaveBeenCalledWith("aria-selected");
    expect(loadedPolicy.count).toHaveBeenCalledOnce();
    expect(result.requests).toEqual([
      {
        method: "GET",
        pathname: "/eshu-api/api/v0/auth/admin/sign-in-policy",
        status: 200,
      },
    ]);
    expect(result.passed).toBe(true);
  });

  it("fails the Admin click workflow when only a neighboring response succeeds", async () => {
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const neighboringResponse: ResponseStub = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/auth/admin/sign-in-policy-preview",
    };
    const page = {
      locator: vi.fn(() => locatorStub()),
      getByRole: vi.fn(() => tab),
      waitForResponse: vi.fn(async (predicate: (candidate: ResponseStub) => boolean) => {
        if (!predicate(neighboringResponse)) throw new Error("no matching response");
        return neighboringResponse;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
        loadedStateSelector: "#policy-require-sso",
        expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("no matching response");
  });

  it("fails the Admin click workflow when the policy API succeeds but the UI is unavailable", async () => {
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("true") });
    const main = locatorStub({
      textContent: vi.fn().mockResolvedValue("Sign-in policy unavailable from this source."),
    });
    const response: ResponseStub = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/auth/admin/sign-in-policy",
    };
    const page = {
      locator: vi.fn((selector: string) =>
        selector === ".main" ? main : locatorStub(),
      ),
      getByRole: vi.fn(() => tab),
      waitForResponse: vi.fn(async () => response),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
        loadedStateSelector: "#policy-require-sso",
        expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
        forbiddenText: "Sign-in policy unavailable from this source.",
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("rendered forbidden state");
  });

  it("fails a click workflow when the named tab never becomes selected", async () => {
    const main = locatorStub();
    const tab = locatorStub({ getAttribute: vi.fn().mockResolvedValue("false") });
    const response: ResponseStub = {
      request: () => ({ method: () => "GET" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/auth/admin/sign-in-policy",
    };
    const page = {
      locator: vi.fn(() => main),
      getByRole: vi.fn(() => tab),
      waitForResponse: vi.fn(async () => response),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "admin-policy",
        kind: "click",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
        loadedStateSelector: "#policy-require-sso",
        expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
      },
      vi.fn(),
    );

    expect(result.passed).toBe(false);
    expect(result.detail).toContain("aria-selected");
  });

  it("does not inspect a submit outcome before the matching response arrives", async () => {
    const field = locatorStub({ inputValue: vi.fn().mockResolvedValue("local") });
    const outcome = locatorStub();
    const submit = locatorStub();
    const response: ResponseStub = {
      request: () => ({ method: () => "POST" }),
      status: () => 200,
      url: () => "http://host/eshu-api/api/v0/cloud/runtime-drift/findings",
    };
    let releaseResponse: ((value: ResponseStub) => void) | undefined;
    const delayedResponse = new Promise<ResponseStub>((resolve) => {
      releaseResponse = resolve;
    });
    const page = {
      locator: vi.fn((selector: string) => (selector === "#scope" ? field : outcome)),
      getByRole: vi.fn(() => submit),
      waitForResponse: vi.fn((predicate: (candidate: ResponseStub) => boolean) => {
        expect(predicate(response)).toBe(true);
        return delayedResponse;
      }),
    } as unknown as Page;

    let settled = false;
    const resultPromise = executeRouteWorkflow(
      page,
      {
        id: "cloud-drift-submit",
        kind: "submit",
        fields: [{ selector: "#scope", value: "local" }],
        role: "button",
        name: "Load drift findings",
        expectedRequestPath: "/api/v0/cloud/runtime-drift/findings",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".evidence-workbench > .panel:first-child tbody tr",
      },
      vi.fn(),
    ).then((result) => {
      settled = true;
      return result;
    });

    await vi.waitFor(() => expect(submit.click).toHaveBeenCalledOnce());
    expect(settled).toBe(false);
    expect(outcome.count).not.toHaveBeenCalled();

    releaseResponse?.(response);
    const result = await resultPromise;
    expect(result.passed).toBe(true);
    expect(outcome.count).toHaveBeenCalledOnce();
  });

  it("requires every Cloud Drift response and response-backed surface", async () => {
    const field = locatorStub({ inputValue: vi.fn().mockResolvedValue("aws:scope:one") });
    const outcome = locatorStub();
    const submit = locatorStub();
    const paths = [
      "/api/v0/cloud/runtime-drift/findings",
      "/api/v0/aws/runtime-drift/findings",
      "/api/v0/iac/unmanaged-resources",
      "/api/v0/iac/terraform-import-plan/candidates",
    ];
    const responses = paths.map<ResponseStub>((path) => ({
      request: () => ({ method: () => "POST" }),
      status: () => 200,
      url: () => `http://host/eshu-api${path}`,
    }));
    const page = {
      locator: vi.fn((selector: string) => (selector === "#scope" ? field : outcome)),
      getByRole: vi.fn(() => submit),
      waitForResponse: vi.fn(async (predicate: (candidate: ResponseStub) => boolean) => {
        const response = responses.find((candidate) => predicate(candidate));
        if (!response) throw new Error("no matching response");
        return response;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "cloud-drift-all-surfaces",
        kind: "submit",
        fields: [{ selector: "#scope", value: "aws:scope:one" }],
        role: "button",
        name: "Load drift findings",
        expectedRequestPath: paths[0],
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        additionalExpectedRequests: paths.slice(1).map((path) => ({
          path,
          method: "POST" as const,
          acceptedStatuses: [200],
        })),
        outcomeSelector: ".multi tbody tr",
        additionalOutcomeSelectors: [
          ".aws tbody tr",
          ".unmanaged tbody tr",
          '[data-import-plan-status="loaded"]',
        ],
      },
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(result.requests).toHaveLength(4);
    expect(result.dataShapes).toHaveLength(4);
    expect(page.waitForResponse).toHaveBeenCalledTimes(4);
  });

  it.each([
    { currentPath: "/incidents", shouldPass: false },
    { currentPath: "/incidents/PABC123/context", shouldPass: true },
  ])(
    "requires the incident submit to reach its parameterized page path from $currentPath",
    async ({ currentPath, shouldPass }) => {
      vi.stubEnv("ESHU_E2E_INCIDENT_ID", "PABC123");
      const incident = locatorStub({ inputValue: vi.fn().mockResolvedValue("PABC123") });
      const outcome = locatorStub();
      const submit = locatorStub();
      const response: ResponseStub = {
        request: () => ({ method: () => "GET" }),
        status: () => 200,
        url: () => "http://host/eshu-api/api/v0/incidents/PABC123/context",
      };
      const page = {
        locator: vi.fn((selector: string) => (selector === "#incident" ? incident : outcome)),
        getByRole: vi.fn(() => submit),
        waitForResponse: vi.fn(async () => response),
        url: vi.fn(() => `http://host${currentPath}`),
      } as unknown as Page;

      const result = await executeRouteWorkflow(
        page,
        {
          id: "incident-submit",
          kind: "submit",
          fields: [{ selector: "#incident", valueEnv: "ESHU_E2E_INCIDENT_ID" }],
          role: "button",
          name: "Review incident",
          expectedRequestPath: "/api/v0/incidents/${ESHU_E2E_INCIDENT_ID}/context",
          expectedRequestMethod: "GET",
          acceptedResponseStatuses: [200],
          expectedPagePath: "/incidents/${ESHU_E2E_INCIDENT_ID}/context",
          outcomeSelector: ".incident-summary",
        },
        vi.fn(),
      );

      expect(result.passed).toBe(shouldPass);
      if (shouldPass) {
        expect(outcome.count).toHaveBeenCalledOnce();
      } else {
        expect(result.detail).toContain("/incidents/PABC123/context");
        expect(outcome.count).not.toHaveBeenCalled();
      }
      vi.unstubAllEnvs();
    },
  );
});

describe("repositoryPathsFromSourceHref", () => {
  it("derives repository API requests under the versioned API prefix", () => {
    expect(repositoryApiPathFromSourcePath("/repositories/repository%3Ar_123/source")).toBe(
      "/api/v0/repositories/repository%3Ar_123",
    );
  });

  it("derives source and workspace routes from the same retained repository id", () => {
    expect(repositoryPathsFromSourceHref("/repositories/repository%3Ar_123/source")).toEqual({
      sourcePath: "/repositories/repository%3Ar_123/source",
      workspacePath: "/workspace/repositories/repository%3Ar_123",
    });
  });

  it("rejects a generic or malformed route instead of inventing an id", () => {
    expect(repositoryPathsFromSourceHref("/repositories/source")).toBeNull();
    expect(repositoryPathsFromSourceHref("/workspace/repositories/repository%3Ar_123")).toBeNull();
  });
});
