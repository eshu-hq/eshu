import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { consoleRoutes } from "../src/e2e/routeAssertions";
import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub, type ResponseStub } from "./routeWorkflowProbesTestSupport";

describe("retained Semantic Search workflow", () => {
  it("rejects a successful exact-empty announcement without a visible retained result", async () => {
    const previousRepository = process.env.ESHU_E2E_SEMANTIC_REPOSITORY_ID;
    const previousQuery = process.env.ESHU_E2E_SEMANTIC_QUERY;
    process.env.ESHU_E2E_SEMANTIC_REPOSITORY_ID = "repository:retained";
    process.env.ESHU_E2E_SEMANTIC_QUERY = "retained symbol";

    try {
      const repository = locatorStub({
        inputValue: vi.fn().mockResolvedValue("repository:retained"),
      });
      const query = locatorStub({ inputValue: vi.fn().mockResolvedValue("retained symbol") });
      const form = locatorStub();
      const submit = locatorStub();
      form.getByRole.mockReturnValue(submit);
      const emptyRows = locatorStub({
        count: vi.fn().mockResolvedValue(0),
        isVisible: vi.fn().mockResolvedValue(false),
      });
      const announcement = locatorStub({ textContent: vi.fn().mockResolvedValue("0 results") });
      const response: ResponseStub = {
        request: () => ({
          method: () => "POST",
          postDataJSON: () => ({
            query: "retained symbol",
            repo_id: "repository:retained",
          }),
        }),
        status: () => 200,
        url: () => "http://host/eshu-api/api/v0/search/semantic",
      };
      const page = {
        locator: vi.fn((selector: string) => {
          if (selector === 'input[aria-label="Repository"]') return repository;
          if (selector === 'input[aria-label="Search query"]') return query;
          if (selector === ".semantic-search-form") return form;
          if (selector === ".sem-result-row") return emptyRows;
          if (selector === ".sem-result-announce") return announcement;
          return locatorStub();
        }),
        getByRole: vi.fn(() => submit),
        waitForResponse: vi.fn(async () => response),
      } as unknown as Page;
      const workflow = consoleRoutes.find((route) => route.path === "/semantic-search")?.workflow;
      if (!workflow) throw new Error("semantic-search workflow is missing");

      const result = await executeRouteWorkflow(page, workflow, vi.fn());

      expect(result.passed).toBe(false);
      expect(result.detail).toContain(".sem-result-row");
      expect(announcement.textContent).not.toHaveBeenCalled();
    } finally {
      restoreEnvironment("ESHU_E2E_SEMANTIC_REPOSITORY_ID", previousRepository);
      restoreEnvironment("ESHU_E2E_SEMANTIC_QUERY", previousQuery);
    }
  });
});

function restoreEnvironment(name: string, value: string | undefined): void {
  if (value === undefined) {
    delete process.env[name];
    return;
  }
  process.env[name] = value;
}
