import type { Page, Response } from "playwright";

import type { RouteWorkflowObservation, RouteWorkflowSpec } from "../src/e2e/routeAssertions.ts";
import {
  dataShape,
  failed,
  forbiddenState,
  matchesExpectedResponse,
  passed,
  requestObservation,
  visibleCount,
  type WaitForApiQuiet,
} from "./routeWorkflowProbeSupport.ts";

const inventoryResponse = {
  acceptedStatuses: [200],
  method: "POST" as const,
  path: "/api/v0/code/structure/inventory",
};
const storyResponse = {
  acceptedStatuses: [200],
  method: "POST" as const,
  path: "/api/v0/code/relationships/story",
};
const importsResponse = {
  acceptedStatuses: [200],
  method: "POST" as const,
  path: "/api/v0/code/imports/investigate",
};

interface SelectOption {
  readonly label: string;
  readonly value: string;
}

export async function executeCodeGraphControls(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "state" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const controls = workflow.codeGraphControls;
  if (!controls) return failed(workflow.id, "Code Graph controls are not configured");
  const repository = page.locator(controls.repositorySelector);
  const symbol = page.locator(controls.symbolSelector);
  const options = await selectOptions(repository);
  if (options.length < 2) {
    return failed(
      workflow.id,
      `Code Graph proof requires two authorized repositories; found ${options.length}`,
    );
  }
  const initialRepoId = await repository.inputValue();
  const searchTarget = options.find((option) => option.value !== initialRepoId) ?? options[1];
  if (!searchTarget) return failed(workflow.id, "Code Graph repository search target is missing");

  const globalSearch = page.locator(controls.globalSearchSelector);
  const firstReads = await captureRepositoryReads(page, waitForQuiet, async () => {
    await globalSearch.fill(searchTarget.value);
    await globalSearch.press("Enter");
  });
  const firstEntityId = await symbol.inputValue();
  if (!hasRequiredRepositoryReads(firstReads, firstEntityId !== "")) {
    return failed(workflow.id, "searched repository did not complete its bounded Code Graph reads");
  }
  if (!repositoryReadsMatch(firstReads, searchTarget.value)) {
    return failed(workflow.id, "Code Graph requests did not retain searched repository scope");
  }
  const searchFailure = await verifyRepositoryScope(page, repository, searchTarget.value);
  if (searchFailure) return failed(workflow.id, searchFailure);

  const switchTarget = options.find((option) => option.value !== searchTarget.value);
  if (!switchTarget) return failed(workflow.id, "Code Graph switch target is missing");
  const secondReads = await captureRepositoryReads(page, waitForQuiet, async () => {
    await repository.selectOption(switchTarget.value);
  });
  const switchedEntityId = await symbol.inputValue();
  if (!hasRequiredRepositoryReads(secondReads, switchedEntityId !== "")) {
    return failed(workflow.id, "switched repository did not complete its bounded Code Graph reads");
  }
  if (!repositoryReadsMatch(secondReads, switchTarget.value)) {
    return failed(workflow.id, "Code Graph requests did not retain switched repository scope");
  }
  const switchFailure = await verifyRepositoryScope(page, repository, switchTarget.value);
  if (switchFailure) return failed(workflow.id, switchFailure);
  const switchedEntities = (await selectOptions(symbol)).map((option) => option.value);
  if (firstEntityId && switchedEntities.includes(firstEntityId)) {
    return failed(workflow.id, "prior-repository symbol remained after repository switch");
  }

  const symbolOptions = await selectOptions(symbol);
  const currentEntityId = await symbol.inputValue();
  const symbolTarget = symbolOptions.find((option) => option.value !== currentEntityId);
  let symbolStory: Response | null = null;
  if (symbolTarget) {
    const storyPromise = waitForResponse(page, storyResponse);
    await symbol.selectOption(symbolTarget.value);
    symbolStory = await storyPromise;
    await waitForQuiet();
    if ((await symbol.inputValue()) !== symbolTarget.value) {
      return failed(workflow.id, "Code Graph symbol selection did not remain selected");
    }
  }
  const graphCount = await visibleCount(page.locator(controls.graphSelector));
  if (graphCount === 0)
    return failed(workflow.id, "Code Graph canvas disappeared after drill-down");
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  return passed(
    workflow.id,
    "searched, switched, and drilled into repository-owned Code Graph state",
    [dataShape(controls.graphSelector, graphCount)],
    [...firstReads, ...secondReads, ...(symbolStory ? [symbolStory] : [])].map(requestObservation),
  );
}

async function verifyRepositoryScope(
  page: Page,
  repository: ReturnType<Page["locator"]>,
  expectedRepoId: string,
): Promise<string> {
  const urlRepoId = new URL(page.url()).searchParams.get("repo_id");
  if (urlRepoId !== expectedRepoId)
    return `Code Graph URL selected ${urlRepoId ?? "none"}, expected ${expectedRepoId}`;
  const selectedRepoId = await repository.inputValue();
  return selectedRepoId === expectedRepoId
    ? ""
    : `Code Graph selector selected ${selectedRepoId || "none"}, expected ${expectedRepoId}`;
}

async function captureRepositoryReads(
  page: Page,
  waitForQuiet: WaitForApiQuiet,
  action: () => Promise<void>,
): Promise<readonly Response[]> {
  const responses: Response[] = [];
  const record = (response: Response): void => {
    if (
      [inventoryResponse, storyResponse, importsResponse].some((expected) =>
        matchesExpectedResponse(
          response,
          expected.path,
          expected.method,
          expected.acceptedStatuses,
        ),
      )
    ) {
      responses.push(response);
    }
  };
  page.on("response", record);
  try {
    await action();
    await waitForQuiet();
  } finally {
    page.off("response", record);
  }
  return responses;
}

function hasRequiredRepositoryReads(
  responses: readonly Response[],
  expectsStory: boolean,
): boolean {
  const has = (expected: typeof inventoryResponse): boolean =>
    responses.some((response) =>
      matchesExpectedResponse(response, expected.path, expected.method, expected.acceptedStatuses),
    );
  return has(inventoryResponse) && has(importsResponse) && (!expectsStory || has(storyResponse));
}

function repositoryReadsMatch(responses: readonly Response[], expectedRepoId: string): boolean {
  return responses.every((response) => {
    try {
      const body = response.request().postDataJSON() as { readonly repo_id?: unknown };
      return body.repo_id === expectedRepoId;
    } catch {
      return false;
    }
  });
}

function waitForResponse(
  page: Page,
  expected: {
    readonly acceptedStatuses: readonly number[];
    readonly method: "POST";
    readonly path: string;
  },
): Promise<Response> {
  return page.waitForResponse((response) =>
    matchesExpectedResponse(response, expected.path, expected.method, expected.acceptedStatuses),
  );
}

async function selectOptions(
  select: ReturnType<Page["locator"]>,
): Promise<readonly SelectOption[]> {
  return select.locator("option").evaluateAll((options) =>
    options
      .map((option) => ({
        label: option.textContent?.trim() ?? "",
        value: (option as HTMLOptionElement).value,
      }))
      .filter((option) => option.value !== ""),
  );
}
