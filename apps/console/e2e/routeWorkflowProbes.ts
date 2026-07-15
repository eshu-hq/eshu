import type { Page } from "playwright";

import type { RouteWorkflowObservation, RouteWorkflowSpec } from "../src/e2e/routeAssertions.ts";

type WaitForApiQuiet = () => Promise<void>;

async function forbiddenTextPresent(
  page: Page,
  forbiddenText: string | undefined,
): Promise<boolean> {
  if (!forbiddenText) {
    return false;
  }
  const mainText = (await page.locator(".main").textContent()) ?? "";
  return mainText.includes(forbiddenText);
}

function failed(id: string, detail: string): RouteWorkflowObservation {
  return { id, passed: false, detail };
}

async function executeStateWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "state" }>,
): Promise<RouteWorkflowObservation> {
  for (const selector of workflow.anySelectors) {
    if ((await page.locator(selector).count()) > 0) {
      if (await forbiddenTextPresent(page, workflow.forbiddenText)) {
        return failed(workflow.id, `rendered forbidden state: ${workflow.forbiddenText}`);
      }
      return { id: workflow.id, passed: true, detail: `rendered ${selector}` };
    }
  }
  return failed(
    workflow.id,
    `none of the required live selectors rendered: ${workflow.anySelectors.join(", ")}`,
  );
}

async function executeFillWorkflow(
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
  const response = workflow.expectedRequestPath
    ? page.waitForResponse((candidate) => {
        try {
          return new URL(candidate.url()).pathname.endsWith(workflow.expectedRequestPath ?? "");
        } catch {
          return false;
        }
      })
    : null;
  await input.fill(workflow.value);
  await response;
  await waitForQuiet();
  if ((await input.inputValue()) !== workflow.value) {
    return failed(workflow.id, `control did not retain value ${workflow.value}`);
  }
  if (outcome && (await outcome.count()) === 0) {
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
  if (await forbiddenTextPresent(page, workflow.forbiddenText)) {
    return failed(workflow.id, `rendered forbidden state: ${workflow.forbiddenText}`);
  }
  return { id: workflow.id, passed: true, detail: `filled ${workflow.selector}` };
}

async function executeSubmitWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "submit" }>,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  for (const field of workflow.fields) {
    const control = page.locator(field.selector);
    const count = await control.count();
    if (count !== 1) {
      return failed(workflow.id, `expected one ${field.selector} control; found ${count}`);
    }
    await control.fill(field.value);
    if ((await control.inputValue()) !== field.value) {
      return failed(workflow.id, `control ${field.selector} did not retain its bounded value`);
    }
  }

  const submit = page.getByRole(workflow.role, { name: workflow.name, exact: true });
  const count = await submit.count();
  if (count !== 1) {
    return failed(
      workflow.id,
      `expected one ${workflow.role} named ${workflow.name}; found ${count}`,
    );
  }
  const response = page.waitForResponse((candidate) => {
    try {
      return new URL(candidate.url()).pathname.endsWith(workflow.expectedRequestPath);
    } catch {
      return false;
    }
  });
  await submit.click();
  await response;
  await waitForQuiet();

  if ((await page.locator(workflow.outcomeSelector).count()) === 0) {
    return failed(workflow.id, `outcome did not render at ${workflow.outcomeSelector}`);
  }
  if (await forbiddenTextPresent(page, workflow.forbiddenText)) {
    return failed(workflow.id, `rendered forbidden state: ${workflow.forbiddenText}`);
  }
  return { id: workflow.id, passed: true, detail: `submitted ${workflow.name}` };
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

  await control.click();
  await waitForQuiet();
  if ((await control.getAttribute("aria-selected")) !== "true") {
    return failed(workflow.id, `${workflow.name} did not become aria-selected`);
  }
  if ((await page.locator(workflow.outcomeSelector).count()) === 0) {
    return failed(workflow.id, `outcome did not render at ${workflow.outcomeSelector}`);
  }
  if (await forbiddenTextPresent(page, workflow.forbiddenText)) {
    return failed(workflow.id, `rendered forbidden state: ${workflow.forbiddenText}`);
  }
  return { id: workflow.id, passed: true, detail: `clicked ${workflow.name}` };
}

// executeRouteWorkflow performs one bounded, declarative route probe. Failures
// are observations rather than thrown errors so the live report preserves the
// route and reason while continuing through the remaining console surfaces.
export async function executeRouteWorkflow(
  page: Page,
  workflow: RouteWorkflowSpec,
  waitForQuiet: WaitForApiQuiet,
): Promise<RouteWorkflowObservation> {
  try {
    switch (workflow.kind) {
      case "state":
        return await executeStateWorkflow(page, workflow);
      case "fill":
        return await executeFillWorkflow(page, workflow, waitForQuiet);
      case "click":
        return await executeClickWorkflow(page, workflow, waitForQuiet);
      case "submit":
        return await executeSubmitWorkflow(page, workflow, waitForQuiet);
    }
  } catch (error) {
    return failed(workflow.id, error instanceof Error ? error.message : String(error));
  }
}
