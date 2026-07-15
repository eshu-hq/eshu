import type { Page } from "playwright";

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

export interface RequestAnchor {
  readonly key: string;
  readonly value: string;
}

export function requestAnchorFailure(
  response: Awaited<ReturnType<Page["waitForResponse"]>>,
  anchors: readonly RequestAnchor[],
): string | null {
  if (anchors.length === 0) return null;
  const method = response.request().method().toUpperCase();
  if (method === "GET") {
    const searchParams = new URL(response.url()).searchParams;
    for (const anchor of anchors) {
      const values = searchParams.getAll(anchor.key);
      if (values.length !== 1 || values[0] !== anchor.value) {
        return `request did not preserve exact query anchor ${anchor.key}`;
      }
    }
    return null;
  }

  let body: unknown;
  try {
    body = response.request().postDataJSON();
  } catch {
    return "request body was not valid JSON";
  }
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    return "request body was not a JSON object";
  }
  const record = body as Readonly<Record<string, unknown>>;
  for (const anchor of anchors) {
    if (record[anchor.key] !== anchor.value) {
      return `request did not preserve exact JSON anchor ${anchor.key}`;
    }
  }
  return null;
}

export async function executeFillWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "fill" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  const input = page.locator(workflow.selector);
  const count = await input.count();
  if (count !== 1) {
    return failed(workflow.id, `expected one ${workflow.selector} control; found ${count}`);
  }

  const outcome = workflow.outcomeSelector ? page.locator(workflow.outcomeSelector) : null;
  const before = workflow.requireOutcomeChange ? await outcome?.textContent() : null;
  if (
    workflow.expectedRequestPath &&
    (!workflow.expectedRequestMethod || !workflow.acceptedResponseStatuses?.length)
  ) {
    return failed(workflow.id, "expected request proof requires method and accepted statuses");
  }
  const responsePromise = workflow.expectedRequestPath
    ? page.waitForResponse((candidate) =>
        matchesExpectedResponse(
          candidate,
          workflow.expectedRequestPath ?? "",
          workflow.expectedRequestMethod ?? "GET",
          workflow.acceptedResponseStatuses ?? [],
        ),
      )
    : null;
  await input.fill(workflow.value);
  const response = responsePromise ? await responsePromise : null;
  await waitForQuiet();
  if ((await input.inputValue()) !== workflow.value) {
    return failed(workflow.id, `control did not retain value ${workflow.value}`);
  }
  const outcomeCount = outcome ? await visibleCount(outcome) : 0;
  if (outcome && outcomeCount === 0) {
    return failed(workflow.id, `no outcome rendered at ${workflow.outcomeSelector}`);
  }
  if (workflow.requireOutcomeChange && (await outcome?.textContent()) === before) {
    return failed(workflow.id, `outcome at ${workflow.outcomeSelector} did not change`);
  }
  if (workflow.outcomeTextIncludes) {
    const outcomeText = (await outcome?.textContent()) ?? "";
    if (!outcomeText.includes(workflow.outcomeTextIncludes)) {
      return failed(
        workflow.id,
        `outcome at ${workflow.outcomeSelector} did not include ${workflow.outcomeTextIncludes}`,
      );
    }
  }
  if (response && workflow.requestKey) {
    const requestFailure = requestAnchorFailure(response, [
      { key: workflow.requestKey, value: workflow.value },
    ]);
    if (requestFailure) return failed(workflow.id, requestFailure);
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  return passed(
    workflow.id,
    `filled ${workflow.selector}`,
    outcome ? [dataShape(workflow.outcomeSelector ?? workflow.selector, outcomeCount)] : [],
    response ? [requestObservation(response)] : [],
  );
}
