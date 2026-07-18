import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub, type ResponseStub } from "./routeWorkflowProbesTestSupport";

describe("retained Replatforming workflow", () => {
  it("discovers rendered selectors and proves next-page requests", async () => {
    const account = locatorStub({ inputValue: vi.fn().mockResolvedValue("123456789012") });
    const accountOption = locatorStub({
      getAttribute: vi.fn().mockResolvedValue("123456789012"),
    });
    const region = locatorStub({ inputValue: vi.fn().mockResolvedValue("us-east-1") });
    const regionOption = locatorStub({ getAttribute: vi.fn().mockResolvedValue("us-east-1") });
    const submit = locatorStub();
    const next = locatorStub();
    const previous = locatorStub({ getAttribute: vi.fn().mockResolvedValue(null) });
    const outcome = locatorStub();
    const scope = locatorStub({
      getByRole: vi.fn(() => submit),
    });
    let currentURL = "http://host/replatforming";
    next.click.mockImplementation(async () => {
      currentURL = "http://host/replatforming?scope_kind=account&account_id=redacted&offset=100";
    });

    const paths = ["rollups", "plans", "ownership-packets"];
    const responses: ResponseStub[] = [...paths, ...paths].map((path, index) => ({
      request: () => ({
        method: () => "POST",
        postDataJSON: () => ({
          account_id: "123456789012",
          offset: index < paths.length ? 0 : 100,
          region: "us-east-1",
        }),
      }),
      status: () => 200,
      url: () => `http://host/eshu-api/api/v0/replatforming/${path}`,
    }));
    const page = {
      getByRole: vi.fn((_role: string, options: { readonly name: string }) =>
        options.name === "Next page" ? next : previous,
      ),
      locator: vi.fn((selector: string) => {
        if (selector === "#account") return account;
        if (selector === "#accounts option") return accountOption;
        if (selector === "#region") return region;
        if (selector === "#regions option") return regionOption;
        if (selector === ".query") return scope;
        return outcome;
      }),
      url: vi.fn(() => currentURL),
      waitForResponse: vi.fn(async (predicate: (candidate: ResponseStub) => boolean) => {
        const response = responses.shift();
        if (!response || !predicate(response)) throw new Error("no matching response");
        return response;
      }),
    } as unknown as Page;

    const result = await executeRouteWorkflow(
      page,
      {
        id: "replatforming-visible-selectors",
        kind: "submit",
        fields: [
          {
            requestKey: "account_id",
            selector: "#account",
            valueFromSelector: "#accounts option",
          },
          {
            requestKey: "region",
            selector: "#region",
            valueFromSelector: "#regions option",
          },
        ],
        role: "button",
        name: "Review plan",
        scopeSelector: ".query",
        expectedRequestPath: "/api/v0/replatforming/rollups",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        additionalExpectedRequests: paths.slice(1).map((path) => ({
          path: `/api/v0/replatforming/${path}`,
          method: "POST" as const,
          acceptedStatuses: [200],
        })),
        outcomeSelector: ".truth",
        pagination: {
          nextName: "Next page",
          previousName: "Previous page",
          expectedRequests: paths.map((path) => ({
            path: `/api/v0/replatforming/${path}`,
            method: "POST" as const,
            acceptedStatuses: [200],
          })),
          offsetQueryKey: "offset",
        },
      } as never,
      vi.fn(),
    );

    expect(result.passed).toBe(true);
    expect(account.fill).toHaveBeenCalledWith("123456789012");
    expect(region.fill).toHaveBeenCalledWith("us-east-1");
    expect(next.click).toHaveBeenCalledOnce();
    expect(previous.getAttribute).toHaveBeenCalledWith("disabled");
    expect(result.requests).toHaveLength(6);
  });
});
