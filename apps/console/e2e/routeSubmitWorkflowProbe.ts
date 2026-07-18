import type { Page } from "playwright";

import type {
  RouteWorkflowObservation,
  RouteWorkflowSpec,
  WorkflowDataShapeObservation,
  WorkflowRequestObservation,
} from "../src/e2e/routeAssertions.ts";
import type { WorkflowField } from "../src/e2e/consoleRouteCatalogTypes.ts";
import {
  dataShape,
  failed,
  forbiddenState,
  matchesWorkflowResponse,
  passed,
  pathname,
  requestObservation,
  retainedEnvironmentValue,
  resolveWorkflowTemplate,
  visibleCount,
  type WaitForApiQuiet,
} from "./routeWorkflowProbeSupport.ts";
import { requestAnchorFailure, type RequestAnchor } from "./routeFillWorkflowProbe.ts";

type SubmitWorkflow = Extract<RouteWorkflowSpec, { readonly kind: "submit" }>;

interface FieldValueResult {
  readonly error: string | null;
  readonly value: string;
}

async function resolveFieldValue(page: Page, field: WorkflowField): Promise<FieldValueResult> {
  if (field.value !== undefined) return { error: null, value: field.value };
  if (field.valueEnv !== undefined) {
    const value = retainedEnvironmentValue(field.valueEnv);
    return value === null
      ? { error: `required retained-data anchor ${field.valueEnv} is missing`, value: "" }
      : { error: null, value };
  }
  const option = page.locator(field.valueFromSelector).first();
  await option.waitFor({ state: "attached", timeout: 10_000 });
  const value = (await option.getAttribute("value"))?.trim() ?? "";
  return value === ""
    ? { error: `no bounded value was rendered at ${field.valueFromSelector}`, value: "" }
    : { error: null, value };
}

async function fillFields(
  page: Page,
  workflow: SubmitWorkflow,
): Promise<{ readonly anchors: RequestAnchor[]; readonly error: string | null }> {
  const anchors: RequestAnchor[] = [];
  for (const field of workflow.fields) {
    const resolved = await resolveFieldValue(page, field);
    if (resolved.error) return { anchors, error: resolved.error };
    const control = page.locator(field.selector);
    const count = await control.count();
    if (count !== 1) {
      return { anchors, error: `expected one ${field.selector} control; found ${count}` };
    }
    if (field.interaction === "select") await control.selectOption(resolved.value);
    else await control.fill(resolved.value);
    if ((await control.inputValue()) !== resolved.value) {
      return { anchors, error: `control ${field.selector} did not retain its bounded value` };
    }
    if (field.requestKey) anchors.push({ key: field.requestKey, value: resolved.value });
  }
  return { anchors, error: null };
}

async function provePagination(
  page: Page,
  workflow: SubmitWorkflow,
  waitForQuiet: WaitForApiQuiet,
): Promise<
  | { readonly error: string; readonly requests: readonly WorkflowRequestObservation[] }
  | { readonly error: null; readonly requests: readonly WorkflowRequestObservation[] }
> {
  const pagination = workflow.pagination;
  if (!pagination) return { error: null, requests: [] };
  const next = page.getByRole("button", { name: pagination.nextName, exact: true });
  if ((await next.count()) !== 1 || (await next.getAttribute("disabled")) !== null) {
    return { error: `${pagination.nextName} was not enabled for retained results`, requests: [] };
  }
  const responsePromises = pagination.expectedRequests.map((expected) =>
    page.waitForResponse((candidate) => matchesWorkflowResponse(candidate, expected)),
  );
  await next.click();
  const responses = await Promise.all(responsePromises);
  await waitForQuiet();

  const offset = new URL(page.url()).searchParams.get(pagination.offsetQueryKey) ?? "";
  if (!/^\d+$/.test(offset) || Number(offset) <= 0) {
    return {
      error: `${pagination.nextName} did not publish a positive ${pagination.offsetQueryKey}`,
      requests: [],
    };
  }
  for (const response of responses) {
    let body: unknown;
    try {
      body = response.request().postDataJSON?.();
    } catch {
      return { error: "paginated request body was not valid JSON", requests: [] };
    }
    if (
      typeof body !== "object" ||
      body === null ||
      Array.isArray(body) ||
      String((body as Readonly<Record<string, unknown>>)[pagination.offsetQueryKey]) !== offset
    ) {
      return {
        error: `paginated request did not preserve ${pagination.offsetQueryKey}`,
        requests: [],
      };
    }
  }
  const previous = page.getByRole("button", { name: pagination.previousName, exact: true });
  if ((await previous.count()) !== 1 || (await previous.getAttribute("disabled")) !== null) {
    return { error: `${pagination.previousName} was not enabled after pagination`, requests: [] };
  }
  return { error: null, requests: responses.map(requestObservation) };
}

export async function executeSubmitWorkflow(
  page: Page,
  workflow: SubmitWorkflow,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const fields = await fillFields(page, workflow);
  if (fields.error) return failed(workflow.id, fields.error);
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
  if (count !== 1)
    return failed(
      workflow.id,
      `expected one ${workflow.role} named ${workflow.name}; found ${count}`,
    );
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
    const failure = requestAnchorFailure(response, fields.anchors);
    if (failure) return failed(workflow.id, failure);
  }
  if (expectedPagePath && pathname(page.url()) !== expectedPagePath.value) {
    return failed(
      workflow.id,
      `submitted page reached ${pathname(page.url())}, expected ${expectedPagePath.value}`,
    );
  }
  const selectors = [workflow.outcomeSelector, ...(workflow.additionalOutcomeSelectors ?? [])];
  const shapes: WorkflowDataShapeObservation[] = [];
  for (const selector of selectors) {
    const visible = await visibleCount(page.locator(selector));
    shapes.push(dataShape(selector, visible));
    if (visible === 0) return failed(workflow.id, `outcome did not render at ${selector}`, shapes);
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  const pagination = await provePagination(page, workflow, waitForQuiet);
  if (pagination.error) return failed(workflow.id, pagination.error, shapes);
  return passed(
    workflow.id,
    workflow.pagination
      ? `submitted ${workflow.name} and proved pagination`
      : `submitted ${workflow.name}`,
    shapes,
    [...responses.map(requestObservation), ...pagination.requests],
  );
}
