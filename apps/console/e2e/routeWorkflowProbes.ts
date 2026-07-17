import type { Page } from "playwright";

import type {
  RouteWorkflowObservation,
  RouteWorkflowSpec,
  NetworkObservation,
  WorkflowDataShapeObservation,
  WorkflowRequestObservation,
} from "../src/e2e/routeAssertions.ts";
import {
  dataShape,
  failed,
  forbiddenState,
  matchesExpectedResponse,
  matchesExpectedResponsePrefix,
  matchesWorkflowResponse,
  passed,
  pathname,
  recordedWorkflowResponseProof,
  requestObservation,
  retainedEnvironmentValue,
  resolveWorkflowTemplate,
  visibleCount,
  type WaitForApiQuiet,
} from "./routeWorkflowProbeSupport.ts";
import { executeRepositoryDetailsWorkflow } from "./repositoryRouteWorkflowProbe.ts";
import {
  executeAskExactCountWorkflow,
  type IndexedRepositoryInventoryAnchor,
} from "./askExactCountWorkflowProbe.ts";
import {
  proveVulnerabilityServiceTruth,
  type LoadImpactFindings,
} from "./vulnerabilityRouteWorkflowProbe.ts";
import { executeStateWorkflow } from "./routeStateWorkflowProbe.ts";
import {
  executeFillWorkflow,
  requestAnchorFailure,
  type RequestAnchor,
} from "./routeFillWorkflowProbe.ts";
import { executeDeadCodeControls } from "./deadCodeRouteWorkflowProbe.ts";

export { repositoryPathsFromSourceHref } from "./repositoryRouteWorkflowProbe.ts";

async function executeSubmitWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "submit" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const requestAnchors: RequestAnchor[] = [];
  for (const field of workflow.fields) {
    const value = field.value ?? retainedEnvironmentValue(field.valueEnv);
    if (value === null) {
      return failed(workflow.id, `required retained-data anchor ${field.valueEnv} is missing`);
    }
    const control = page.locator(field.selector);
    const count = await control.count();
    if (count !== 1) {
      return failed(workflow.id, `expected one ${field.selector} control; found ${count}`);
    }
    if (field.interaction === "select") await control.selectOption(value);
    else await control.fill(value);
    if ((await control.inputValue()) !== value) {
      return failed(workflow.id, `control ${field.selector} did not retain its bounded value`);
    }
    if (field.requestKey) requestAnchors.push({ key: field.requestKey, value });
  }

  const expectedPath = resolveWorkflowTemplate(workflow.expectedRequestPath);
  if (expectedPath.missing !== null) {
    return failed(
      workflow.id,
      `required retained-data anchor ${expectedPath.missing} is missing from request path`,
    );
  }
  const expectedPagePath = workflow.expectedPagePath
    ? resolveWorkflowTemplate(workflow.expectedPagePath)
    : null;
  if (expectedPagePath?.missing) {
    return failed(
      workflow.id,
      `required retained-data anchor ${expectedPagePath.missing} is missing from page path`,
    );
  }

  const submit = workflow.scopeSelector
    ? page.locator(workflow.scopeSelector).getByRole(workflow.role, {
        name: workflow.name,
        exact: true,
      })
    : page.getByRole(workflow.role, { name: workflow.name, exact: true });
  await submit.waitFor({ state: "visible", timeout: 10_000 });
  const count = await submit.count();
  if (count !== 1) {
    return failed(
      workflow.id,
      `expected one ${workflow.role} named ${workflow.name}; found ${count}`,
    );
  }
  const expectedRequests = [
    {
      path: expectedPath.value,
      method: workflow.expectedRequestMethod,
      acceptedStatuses: workflow.acceptedResponseStatuses,
    },
    ...(workflow.additionalExpectedRequests ?? []),
  ];
  const responsePromises = expectedRequests.map((expected) =>
    page.waitForResponse((candidate) => matchesWorkflowResponse(candidate, expected)),
  );
  await submit.click();
  const responses = await Promise.all(responsePromises);
  await waitForQuiet();

  for (const response of responses) {
    const requestFailure = requestAnchorFailure(response, requestAnchors);
    if (requestFailure) return failed(workflow.id, requestFailure);
  }

  if (expectedPagePath) {
    const currentPath = pathname(page.url());
    if (currentPath !== expectedPagePath.value) {
      return failed(
        workflow.id,
        `submitted page reached ${currentPath}, expected ${expectedPagePath.value}`,
      );
    }
  }

  const outcomeSelectors = [
    workflow.outcomeSelector,
    ...(workflow.additionalOutcomeSelectors ?? []),
  ];
  const shapes: WorkflowDataShapeObservation[] = [];
  for (const selector of outcomeSelectors) {
    const outcomeCount = await visibleCount(page.locator(selector));
    shapes.push(dataShape(selector, outcomeCount));
    if (outcomeCount === 0) {
      return failed(workflow.id, `outcome did not render at ${selector}`, shapes);
    }
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  return passed(
    workflow.id,
    `submitted ${workflow.name}`,
    shapes,
    responses.map(requestObservation),
  );
}

function kindName(label: string): string {
  return (label.split(" · ")[0] ?? "").trim();
}

async function executeExactKindWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "exactKind" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const controls = page.locator(`${workflow.groupSelector} button`);
  const labels = await controls.allTextContents();
  const available = labels
    .map((label, index) => ({ index, name: kindName(label) }))
    .filter(({ name }) => name.length > 0 && name !== "All kinds");
  const preferred = available.find(
    ({ name }) => name.toLowerCase() === workflow.preferredName.toLowerCase(),
  );
  if (!preferred) {
    return failed(
      workflow.id,
      `required exact kind ${workflow.preferredName} was absent from retained data`,
    );
  }

  const selected = preferred;
  const control = controls.nth(selected.index);
  const responsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponse(
      candidate,
      workflow.expectedRequestPath,
      workflow.expectedRequestMethod,
      workflow.acceptedResponseStatuses,
    ),
  );
  await control.click();
  const response = await responsePromise;
  await waitForQuiet();
  const candidateKind = exactKindRequestCandidate(response);
  if (candidateKind !== workflow.preferredName) {
    return failed(
      workflow.id,
      `exact ${workflow.preferredName} filter sent candidate_kind ${candidateKind ?? "missing or invalid"}`,
    );
  }
  if (!((await control.getAttribute("class")) ?? "").split(/\s+/).includes("active")) {
    return failed(workflow.id, `${selected.name} did not become the active kind filter`);
  }

  const cells = page.locator(workflow.outcomeCellSelector);
  const cellCount = await visibleCount(cells);
  const normalizedKind = selected.name.toLowerCase();
  const cellKinds = (await cells.allTextContents()).map((value) => value.trim().toLowerCase());
  if (cellCount === 0 || cellKinds.some((value) => value !== normalizedKind)) {
    return failed(
      workflow.id,
      `exact ${selected.name} filter returned ${cellCount} row(s) with kinds ${cellKinds.join(", ") || "none"}`,
    );
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  if (workflow.deadCodeControls) {
    return executeDeadCodeControls(
      page,
      workflow,
      workflow.deadCodeControls,
      waitForQuiet,
      cellCount,
      response,
    );
  }
  return passed(
    workflow.id,
    `verified required exact ${workflow.preferredName} filter across ${cellCount} row(s)`,
    [dataShape(workflow.outcomeCellSelector, cellCount)],
    [requestObservation(response)],
  );
}

function exactKindRequestCandidate(
  response: Awaited<ReturnType<Page["waitForResponse"]>>,
): string | null {
  const body = requestBodyRecord(response);
  const candidateKind = body?.candidate_kind;
  return typeof candidateKind === "string" ? candidateKind : null;
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

async function executeClickWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "click" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const control = page.getByRole(workflow.role, { name: workflow.name, exact: true });
  const count = await control.count();
  if (count !== 1) {
    return failed(
      workflow.id,
      `expected one ${workflow.role} named ${workflow.name}; found ${count}`,
    );
  }

  const responsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponse(
      candidate,
      workflow.expectedRequestPath,
      workflow.expectedRequestMethod,
      workflow.acceptedResponseStatuses,
    ),
  );
  await control.click();
  let response: Awaited<ReturnType<Page["waitForResponse"]>>;
  try {
    response = await responsePromise;
  } catch (error) {
    return failed(workflow.id, error instanceof Error ? error.message : "no matching response");
  }
  await waitForQuiet();
  if ((await control.getAttribute("aria-selected")) !== "true") {
    return failed(workflow.id, `${workflow.name} did not become aria-selected`);
  }
  const outcomeCount = await visibleCount(page.locator(workflow.outcomeSelector));
  if (outcomeCount === 0) {
    return failed(workflow.id, `outcome did not render at ${workflow.outcomeSelector}`);
  }
  const loadedStateCount = await visibleCount(page.locator(workflow.loadedStateSelector));
  if (loadedStateCount === 0) {
    return failed(workflow.id, `loaded state did not render at ${workflow.loadedStateSelector}`);
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  return passed(
    workflow.id,
    `clicked ${workflow.name}`,
    [
      dataShape(workflow.outcomeSelector, outcomeCount),
      dataShape(workflow.loadedStateSelector, loadedStateCount),
    ],
    [requestObservation(response)],
  );
}

async function executeTabsWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "tabs" }>,
  waitForQuiet: WaitForApiQuiet,
  network: readonly NetworkObservation[],
  bootstrapNetwork: readonly NetworkObservation[],
  loadImpactFindings?: LoadImpactFindings,
): Promise<RouteWorkflowObservation> {
  const shapes: WorkflowDataShapeObservation[] = [];
  const requests: WorkflowRequestObservation[] = [];
  let serviceTruthDetail = "";
  for (let tabIndex = 0; tabIndex < workflow.tabs.length; tabIndex += 1) {
    const tab = workflow.tabs[tabIndex];
    if (!tab) continue;
    const ownership = recordedWorkflowResponseProof(tab, network, bootstrapNetwork);
    if (!ownership.ok) return failed(workflow.id, ownership.detail, shapes);
    requests.push(...ownership.requests);
    const control = page.getByRole("tab", { name: tab.name, exact: true });
    if ((await visibleCount(control)) !== 1) {
      return failed(workflow.id, `expected one visible tab named ${tab.name}`, shapes);
    }
    await control.click();
    await waitForQuiet();
    if ((await control.getAttribute("aria-selected")) !== "true") {
      return failed(workflow.id, `${tab.name} did not become aria-selected`, shapes);
    }
    const outcomeCount = await visibleCount(page.locator(tab.outcomeSelector));
    shapes.push(dataShape(tab.outcomeSelector, outcomeCount));
    if (outcomeCount === 0) {
      return failed(workflow.id, `${tab.name} did not render ${tab.outcomeSelector}`, shapes);
    }
    const forbidden = await forbiddenState(page, tab);
    if (forbidden) return failed(workflow.id, `${tab.name}: ${forbidden}`, shapes);
    if (tabIndex === 0 && workflow.proveVulnerabilityServiceTruth) {
      if (!loadImpactFindings) {
        return failed(workflow.id, "vulnerability service truth loader is unavailable", shapes);
      }
      const serviceTruth = await proveVulnerabilityServiceTruth(page, loadImpactFindings);
      if (!serviceTruth.ok) return failed(workflow.id, serviceTruth.detail, shapes);
      serviceTruthDetail = `; service truth: ${serviceTruth.detail}`;
    }
  }
  if (!workflow.followLink) {
    return passed(
      workflow.id,
      `proved ${workflow.tabs.length} visible tab surfaces${serviceTruthDetail}`,
      shapes,
      requests,
    );
  }

  const link = page.locator(workflow.followLink.selector).first();
  if ((await visibleCount(link)) !== 1) {
    return failed(
      workflow.id,
      `no retained detail link rendered at ${workflow.followLink.selector}`,
      shapes,
    );
  }
  const responsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponsePrefix(
      candidate,
      workflow.followLink?.expectedRequestPathPrefix ?? "",
      workflow.followLink?.expectedRequestMethod ?? "GET",
      workflow.followLink?.acceptedResponseStatuses ?? [],
    ),
  );
  await link.click();
  const response = await responsePromise;
  await waitForQuiet();
  const currentPath = pathname(page.url());
  if (!currentPath.startsWith(workflow.followLink.expectedPathPrefix)) {
    return failed(
      workflow.id,
      `retained detail link reached ${currentPath}, expected ${workflow.followLink.expectedPathPrefix}`,
      shapes,
    );
  }
  const outcomeCount = await visibleCount(page.locator(workflow.followLink.outcomeSelector));
  shapes.push(dataShape(workflow.followLink.outcomeSelector, outcomeCount));
  if (outcomeCount === 0) {
    return failed(
      workflow.id,
      `retained detail did not render ${workflow.followLink.outcomeSelector}`,
      shapes,
    );
  }
  const forbidden = await forbiddenState(page, workflow.followLink);
  if (forbidden) return failed(workflow.id, `retained detail: ${forbidden}`, shapes);
  return passed(
    workflow.id,
    `proved ${workflow.tabs.length} tab surfaces and one retained detail route${serviceTruthDetail}`,
    shapes,
    [...requests, requestObservation(response)],
  );
}

// executeRouteWorkflow performs one bounded, declarative route probe. Failures
// are observations rather than thrown errors so the live report preserves the
// route and reason while continuing through the remaining console surfaces.
export async function executeRouteWorkflow(
  page: Page,
  workflow: RouteWorkflowSpec,
  waitForQuiet: WaitForApiQuiet,
  network: readonly NetworkObservation[] = [],
  bootstrapNetwork: readonly NetworkObservation[] = [],
  loadImpactFindings?: LoadImpactFindings,
  indexedRepositoryInventory: IndexedRepositoryInventoryAnchor | null = null,
): Promise<RouteWorkflowObservation> {
  try {
    const ownership =
      workflow.kind === "state"
        ? { ok: true as const, requests: [] }
        : recordedWorkflowResponseProof(workflow, network, bootstrapNetwork);
    if (!ownership.ok) return failed(workflow.id, ownership.detail);
    let result: RouteWorkflowObservation;
    switch (workflow.kind) {
      case "state":
        result = await executeStateWorkflow(
          page,
          workflow,
          network,
          bootstrapNetwork,
          waitForQuiet,
        );
        break;
      case "fill":
        result = await executeFillWorkflow(page, workflow, waitForQuiet);
        break;
      case "click":
        result = await executeClickWorkflow(page, workflow, waitForQuiet);
        break;
      case "submit":
        result = await executeSubmitWorkflow(page, workflow, waitForQuiet);
        break;
      case "exactKind":
        result = await executeExactKindWorkflow(page, workflow, waitForQuiet);
        break;
      case "tabs":
        result = await executeTabsWorkflow(
          page,
          workflow,
          waitForQuiet,
          network,
          bootstrapNetwork,
          loadImpactFindings,
        );
        break;
      case "repositoryDetails":
        result = await executeRepositoryDetailsWorkflow(page, workflow, waitForQuiet);
        break;
      case "askExactCount":
        result = await executeAskExactCountWorkflow(
          page,
          workflow,
          waitForQuiet,
          indexedRepositoryInventory,
        );
        break;
    }
    if (ownership.requests.length === 0) return result;
    return { ...result, requests: [...ownership.requests, ...(result.requests ?? [])] };
  } catch (error) {
    return failed(workflow.id, error instanceof Error ? error.message : String(error));
  }
}
