import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub, type ResponseStub } from "./routeWorkflowProbesTestSupport";

describe("executeRouteWorkflow submit controls", () => {
  it("submits a bounded live request with a selected repository", async () => {
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
          { interaction: "select", selector: "#repo", value: "repository:probe" },
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
    expect(repository.selectOption).toHaveBeenCalledWith("repository:probe");
    expect(repository.fill).not.toHaveBeenCalled();
    expect(query.fill).toHaveBeenCalledWith("bounded probe");
    expect(submit.waitFor).toHaveBeenCalledWith({ state: "visible", timeout: 10_000 });
    expect(submit.click).toHaveBeenCalledOnce();
    expect(waitForQuiet).toHaveBeenCalledOnce();
  });
});
