import type { Page } from "playwright";

import type { RouteWorkflowObservation, RouteWorkflowSpec } from "../src/e2e/routeAssertions.ts";
import {
  dataShape,
  failed,
  matchesExpectedResponse,
  passed,
  requestObservation,
  visibleCount,
  type WaitForApiQuiet,
} from "./routeWorkflowProbeSupport.ts";

type ExactKindWorkflow = Extract<RouteWorkflowSpec, { readonly kind: "exactKind" }>;

export async function executeDeadCodeControls(
  page: Page,
  workflow: ExactKindWorkflow,
  controls: NonNullable<ExactKindWorkflow["deadCodeControls"]>,
  waitForQuiet: WaitForApiQuiet,
  exactKindRows: number,
  exactKindResponse: Awaited<ReturnType<Page["waitForResponse"]>>,
): Promise<RouteWorkflowObservation> {
  const shapes = [dataShape(workflow.outcomeCellSelector, exactKindRows)];
  const requests = [requestObservation(exactKindResponse)];
  const reset = page.getByRole("button", { name: controls.resetKindName, exact: true });
  if ((await visibleCount(reset)) !== 1) {
    return failed(workflow.id, `expected one ${controls.resetKindName} reset control`, shapes);
  }
  const resetResponse = await clickForDeadCodeResponse(page, workflow, reset.click.bind(reset));
  await waitForQuiet();
  if (requestBodyRecord(resetResponse)?.candidate_kind !== undefined) {
    return failed(workflow.id, "All kinds reset kept an exact candidate_kind", shapes);
  }
  if ((await reset.getAttribute("aria-pressed")) !== "true") {
    return failed(workflow.id, "All kinds reset did not become visibly selected", shapes);
  }
  requests.push(requestObservation(resetResponse));

  const countScope = page.locator(controls.countScopeSelector);
  if ((await visibleCount(countScope)) !== 1) {
    return failed(workflow.id, "dead-code count-scope wording did not render", shapes);
  }
  const countScopeText = (await countScope.innerText()).trim();
  if (!countScopeText.includes(controls.expectedCountScopeText)) {
    return failed(workflow.id, `dead-code count wording was not window-scoped: ${countScopeText}`);
  }
  shapes.push(dataShape(controls.countScopeSelector, 1));

  const observedLanguages = page.locator(controls.observedLanguageSelector);
  if ((await visibleCount(observedLanguages)) === 0) {
    return failed(workflow.id, "unscoped result returned no observed language to select", shapes);
  }
  const language = (await observedLanguages.first().innerText()).trim();
  if (language === "" || language === "language unavailable") {
    return failed(workflow.id, "unscoped result did not expose a usable language option", shapes);
  }
  const languageOption = page.locator(
    `${controls.languageOptionsSelector}[value=${JSON.stringify(language)}]`,
  );
  if ((await languageOption.count()) !== 1) {
    return failed(
      workflow.id,
      `observed language ${language} was absent from selector options`,
      shapes,
    );
  }
  const languageInput = page.locator(controls.languageSelector);
  await languageInput.fill(language);
  const apply = page.getByRole("button", { name: controls.applyName, exact: true });
  const languageResponse = await clickForDeadCodeResponse(page, workflow, apply.click.bind(apply));
  await waitForQuiet();
  const languageBody = requestBodyRecord(languageResponse);
  if (languageBody?.language !== language || languageBody.repo_id !== undefined) {
    return failed(
      workflow.id,
      "language selection did not apply an exact language-only scope",
      shapes,
    );
  }
  if (new URL(page.url()).searchParams.get("language") !== language) {
    return failed(workflow.id, "language selection did not preserve language in the URL", shapes);
  }
  requests.push(requestObservation(languageResponse));

  const breakdownToggle = page.getByRole("button", {
    name: controls.breakdownToggleName,
    exact: true,
  });
  if ((await visibleCount(breakdownToggle)) !== 1) {
    return failed(workflow.id, "repository breakdown control did not render", shapes);
  }
  await breakdownToggle.click();
  const breakdown = page.locator(controls.breakdownSelector);
  if ((await visibleCount(breakdown)) !== 1) {
    return failed(workflow.id, "repository breakdown did not open", shapes);
  }
  shapes.push(dataShape(controls.breakdownSelector, 1));

  const links = page.locator(controls.breakdownLinkSelector);
  if ((await visibleCount(links)) === 0) {
    return failed(workflow.id, "repository breakdown had no filtered-view link", shapes);
  }
  const link = links.first();
  const href = await link.getAttribute("href");
  const repositoryId = href
    ? new URL(href, "http://console.local").searchParams.get("repo_id")?.trim()
    : "";
  if (!repositoryId) {
    return failed(workflow.id, "repository breakdown link omitted repo_id", shapes);
  }
  if (new URL(href ?? "", "http://console.local").searchParams.get("language") !== language) {
    return failed(workflow.id, "repository breakdown link dropped the active language", shapes);
  }
  const repositoryOption = page.locator(
    `${controls.repositoryOptionsSelector}[value=${JSON.stringify(repositoryId)}]`,
  );
  if ((await repositoryOption.count()) !== 1) {
    return failed(
      workflow.id,
      `repository ${repositoryId} was absent from selector options`,
      shapes,
    );
  }

  const repositoryResponse = await clickForDeadCodeResponse(page, workflow, link.click.bind(link));
  await waitForQuiet();
  const repositoryBody = requestBodyRecord(repositoryResponse);
  if (repositoryBody?.repo_id !== repositoryId || repositoryBody.language !== language) {
    return failed(
      workflow.id,
      "repository breakdown did not preserve canonical repository and language scope",
      shapes,
    );
  }
  const scopedURL = new URL(page.url());
  if (
    scopedURL.searchParams.get("repo_id") !== repositoryId ||
    scopedURL.searchParams.get("language") !== language
  ) {
    return failed(
      workflow.id,
      "repository breakdown did not preserve both scopes in the URL",
      shapes,
    );
  }
  const repositoryInput = page.locator(controls.repositorySelector);
  if ((await repositoryInput.inputValue()) !== repositoryId) {
    return failed(workflow.id, "repository selector did not retain the breakdown scope", shapes);
  }
  if ((await languageInput.inputValue()) !== language) {
    return failed(workflow.id, "language selector did not retain the breakdown scope", shapes);
  }
  requests.push(requestObservation(repositoryResponse));
  return passed(
    workflow.id,
    "verified exact-kind/reset, window-scoped counts, and repository drill-down preserving observed language",
    shapes,
    requests,
  );
}

async function clickForDeadCodeResponse(
  page: Page,
  workflow: ExactKindWorkflow,
  click: () => Promise<void>,
): Promise<Awaited<ReturnType<Page["waitForResponse"]>>> {
  const response = page.waitForResponse((candidate) =>
    matchesExpectedResponse(
      candidate,
      workflow.expectedRequestPath,
      workflow.expectedRequestMethod,
      workflow.acceptedResponseStatuses,
    ),
  );
  await click();
  return response;
}

function requestBodyRecord(
  response: Awaited<ReturnType<Page["waitForResponse"]>>,
): Record<string, unknown> | null {
  let body: unknown;
  try {
    body = response.request().postDataJSON();
  } catch {
    return null;
  }
  if (body === null || typeof body !== "object" || Array.isArray(body)) return null;
  return body as Record<string, unknown>;
}
