import type { Page } from "playwright";

import type {
  NetworkObservation,
  RouteWorkflowObservation,
  WorkflowRequestObservation,
} from "../src/e2e/routeAssertions.ts";
import type { WorkflowResponseExpectation } from "../src/e2e/consoleRouteCatalogTypes.ts";

export type WaitForApiQuiet = () => Promise<void>;

async function forbiddenTextPresent(
  page: Page,
  forbiddenTexts: readonly string[] | undefined,
): Promise<boolean> {
  if (!forbiddenTexts || forbiddenTexts.length === 0) {
    return false;
  }
  const mainText = (await page.locator(".main").textContent()) ?? "";
  return forbiddenTexts.some((text) => mainText.includes(text));
}

export async function visibleCount(locator: ReturnType<Page["locator"]>): Promise<number> {
  const count = await locator.count();
  let visible = 0;
  for (let index = 0; index < count; index += 1) {
    if (await locator.nth(index).isVisible()) visible += 1;
  }
  return visible;
}

async function forbiddenSelectorPresent(
  page: Page,
  selectors: readonly string[] | undefined,
): Promise<string | null> {
  for (const selector of selectors ?? []) {
    if ((await visibleCount(page.locator(selector))) > 0) return selector;
  }
  return null;
}

export function failed(
  id: string,
  detail: string,
  dataShapes: RouteWorkflowObservation["dataShapes"] = [],
): RouteWorkflowObservation {
  return { id, passed: false, detail, dataShapes, requests: [] };
}

export async function forbiddenState(
  page: Page,
  workflow: {
    readonly forbiddenSelectors?: readonly string[];
    readonly forbiddenText?: string;
    readonly forbiddenTexts?: readonly string[];
  },
): Promise<string | null> {
  const selector = await forbiddenSelectorPresent(page, workflow.forbiddenSelectors);
  if (selector) return `visible forbidden selector: ${selector}`;
  const texts = [workflow.forbiddenText, ...(workflow.forbiddenTexts ?? [])].filter(
    (value): value is string => value !== undefined,
  );
  if (await forbiddenTextPresent(page, texts)) {
    return `rendered forbidden state: ${texts.join(", ")}`;
  }
  return null;
}

export function pathname(url: string): string {
  try {
    return new URL(url).pathname;
  } catch {
    return "invalid-url";
  }
}

export function requestObservation(response: Awaited<ReturnType<Page["waitForResponse"]>>) {
  return {
    method: response.request().method(),
    pathname: pathname(response.url()),
    status: response.status(),
  };
}

export function matchesExpectedResponse(
  candidate: Awaited<ReturnType<Page["waitForResponse"]>>,
  expectedPath: string,
  expectedMethod: "GET" | "POST",
  acceptedStatuses: readonly number[],
): boolean {
  try {
    return (
      normalizeConsoleAPIPath(new URL(candidate.url()).pathname) === expectedPath &&
      candidate.request().method().toUpperCase() === expectedMethod &&
      acceptedStatuses.includes(candidate.status())
    );
  } catch {
    return false;
  }
}

function normalizeConsoleAPIPath(pathname: string): string {
  return pathname.startsWith("/eshu-api/") ? pathname.slice("/eshu-api".length) : pathname;
}

function matchesExpectationPath(
  candidatePathname: string,
  expectation: WorkflowResponseExpectation,
): boolean {
  const candidatePath = normalizeConsoleAPIPath(candidatePathname);
  const exactPath = expectation.path;
  if (exactPath !== undefined) return candidatePath === exactPath;

  const prefix = expectation.pathPrefix;
  const suffix = expectation.pathSuffix;
  return (
    prefix !== undefined &&
    suffix !== undefined &&
    candidatePath.startsWith(prefix) &&
    candidatePath.length > prefix.length + suffix.length &&
    candidatePath.endsWith(suffix)
  );
}

export function matchesWorkflowResponse(
  candidate: Awaited<ReturnType<Page["waitForResponse"]>>,
  expectation: WorkflowResponseExpectation,
): boolean {
  try {
    const candidateURL = new URL(candidate.url());
    const matchesQuery = Object.entries(expectation.query ?? {}).every(
      ([key, expectedValue]) => {
        const values = candidateURL.searchParams.getAll(key);
        return values.length === 1 && values[0] === expectedValue;
      },
    );
    return (
      matchesExpectationPath(candidateURL.pathname, expectation) &&
      matchesQuery &&
      candidate.request().method().toUpperCase() === expectation.method &&
      expectation.acceptedStatuses.includes(candidate.status())
    );
  } catch {
    return false;
  }
}

export function recordedResponseProof(
  network: readonly NetworkObservation[],
  expectations: readonly WorkflowResponseExpectation[],
  phase: "bootstrap" | "route" = "route",
):
  | { readonly ok: true; readonly requests: NonNullable<RouteWorkflowObservation["requests"]> }
  | { readonly ok: false; readonly detail: string } {
  const remaining = [...network];
  const requests: WorkflowRequestObservation[] = [];
  for (const expectation of expectations) {
    const index = remaining.findIndex((candidate) => {
      try {
        const candidateURL = new URL(candidate.url);
        const matchesQuery = Object.entries(expectation.query ?? {}).every(
          ([key, expectedValue]) => {
            const values = candidateURL.searchParams.getAll(key);
            return values.length === 1 && values[0] === expectedValue;
          },
        );
        return (
          matchesExpectationPath(candidateURL.pathname, expectation) &&
          matchesQuery &&
          candidate.method.toUpperCase() === expectation.method &&
          expectation.acceptedStatuses.includes(candidate.status) &&
          candidate.failureText === null
        );
      } catch {
        return false;
      }
    });
    if (index < 0) {
      const expectedPath = expectation.path ??
        `${expectation.pathPrefix ?? ""}*${expectation.pathSuffix ?? ""}`;
      return {
        ok: false,
        detail: `required ${phase} response ${expectation.method} ${expectedPath} with status ${expectation.acceptedStatuses.join("/")} was not observed`,
      };
    }
    const [matched] = remaining.splice(index, 1);
    if (!matched) continue;
    requests.push({
      method: matched.method,
      phase,
      pathname: pathname(matched.url),
      status: matched.status,
    });
  }
  return { ok: true, requests };
}

export function matchesExpectedResponsePrefix(
  candidate: Awaited<ReturnType<Page["waitForResponse"]>>,
  expectedPathPrefix: string,
  expectedMethod: "GET" | "POST",
  acceptedStatuses: readonly number[],
): boolean {
  try {
    const requestPath = normalizeConsoleAPIPath(new URL(candidate.url()).pathname);
    const matchesBoundedPrefix = expectedPathPrefix.endsWith("/")
      ? requestPath.startsWith(expectedPathPrefix) && requestPath.length > expectedPathPrefix.length
      : requestPath === expectedPathPrefix || requestPath.startsWith(`${expectedPathPrefix}/`);
    return (
      matchesBoundedPrefix &&
      candidate.request().method().toUpperCase() === expectedMethod &&
      acceptedStatuses.includes(candidate.status())
    );
  } catch {
    return false;
  }
}

export function retainedEnvironmentValue(name: string): string | null {
  const value = process.env[name]?.trim() ?? "";
  return value.length > 0 ? value : null;
}

export function resolveWorkflowTemplate(template: string): {
  value: string;
  missing: string | null;
} {
  let missing: string | null = null;
  const value = template.replace(/\$\{([A-Z0-9_]+)\}/g, (_match, name: string) => {
    const retained = retainedEnvironmentValue(name);
    if (retained === null) {
      missing = name;
      return "";
    }
    return encodeURIComponent(retained);
  });
  return { value, missing };
}

export function passed(
  id: string,
  detail: string,
  dataShapes: RouteWorkflowObservation["dataShapes"],
  requests: RouteWorkflowObservation["requests"] = [],
): RouteWorkflowObservation {
  return { id, passed: true, detail, dataShapes, requests };
}

export function dataShape(selector: string, count: number) {
  return { selector, visibleCount: count };
}
