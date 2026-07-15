import type { Page } from "playwright";

import type { RouteWorkflowObservation, RouteWorkflowSpec } from "../src/e2e/routeAssertions.ts";
import {
  dataShape,
  failed,
  forbiddenState,
  matchesExpectedResponse,
  matchesExpectedResponsePrefix,
  passed,
  pathname,
  requestObservation,
  visibleCount,
  type WaitForApiQuiet,
} from "./routeWorkflowProbeSupport.ts";

export function repositoryPathsFromSourceHref(
  sourceHref: string,
): { sourcePath: string; workspacePath: string } | null {
  const match = sourceHref.match(/^\/repositories\/([^/]+)\/source(?:\?.*)?$/);
  if (!match?.[1]) return null;
  return {
    sourcePath: `/repositories/${match[1]}/source`,
    workspacePath: `/workspace/repositories/${match[1]}`,
  };
}

export function repositoryApiPathFromSourcePath(sourcePath: string): string {
  return `/api/v0${sourcePath.replace(/\/source$/, "")}`;
}

async function navigateClientPath(page: Page, path: string): Promise<void> {
  await page.evaluate((nextPath) => {
    window.history.pushState({}, "", nextPath);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, path);
  await page.waitForFunction((expectedPath) => window.location.pathname === expectedPath, path);
}

export async function executeRepositoryDetailsWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "repositoryDetails" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const gridTab = page.getByRole("button", { name: "Grid", exact: true });
  if ((await visibleCount(gridTab)) !== 1) {
    return failed(workflow.id, "repository grid control was not available");
  }
  await gridTab.click();
  const row = page
    .locator('[aria-label="Repository grid workbench"] tbody tr:not(:has(td.empty))')
    .first();
  if ((await visibleCount(row)) !== 1) {
    return failed(workflow.id, "no retained repository row was available");
  }
  await row.click();
  await waitForQuiet();

  const sourceLink = page.locator(workflow.sourceLinkSelector).first();
  if ((await visibleCount(sourceLink)) !== 1) {
    return failed(
      workflow.id,
      `no retained source link rendered at ${workflow.sourceLinkSelector}`,
    );
  }
  const href = (await sourceLink.getAttribute("href")) ?? "";
  const paths = repositoryPathsFromSourceHref(href);
  if (paths === null) {
    return failed(workflow.id, `retained source link had an invalid path shape: ${pathname(href)}`);
  }

  const repositoryApiPath = repositoryApiPathFromSourcePath(paths.sourcePath);
  const treeResponsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponsePrefix(candidate, `${repositoryApiPath}/tree`, "GET", [200]),
  );
  await sourceLink.click();
  const treeResponse = await treeResponsePromise;
  await waitForQuiet();
  const sourceCount = await visibleCount(page.locator(workflow.sourceOutcomeSelector));
  if (pathname(page.url()) !== paths.sourcePath || sourceCount === 0) {
    return failed(workflow.id, "retained repository source route did not render its file tree");
  }
  const sourceFailure = await forbiddenState(page, {
    forbiddenTexts: ["Failed to load tree:", "File content unavailable from this source."],
  });
  if (sourceFailure) return failed(workflow.id, sourceFailure);

  const storyResponsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponse(candidate, `${repositoryApiPath}/story`, "GET", [200]),
  );
  await navigateClientPath(page, paths.workspacePath);
  const storyResponse = await storyResponsePromise;
  await waitForQuiet();
  const workspaceCount = await visibleCount(page.locator(workflow.workspaceOutcomeSelector));
  if (pathname(page.url()) !== paths.workspacePath || workspaceCount === 0) {
    return failed(
      workflow.id,
      "retained repository workspace route did not render its truth surface",
    );
  }
  const workspaceFailure = await forbiddenState(page, { forbiddenText: "Workspace unavailable" });
  if (workspaceFailure) return failed(workflow.id, workspaceFailure);

  return passed(
    workflow.id,
    "proved retained repository list, source tree, and workspace routes",
    [
      dataShape(workflow.sourceOutcomeSelector, sourceCount),
      dataShape(workflow.workspaceOutcomeSelector, workspaceCount),
    ],
    [requestObservation(treeResponse), requestObservation(storyResponse)],
  );
}
