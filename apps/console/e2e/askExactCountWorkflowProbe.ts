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

export interface IndexedRepositoryInventoryAnchor {
  readonly count: number;
  readonly total: number;
}

type AskExactCountWorkflow = Extract<RouteWorkflowSpec, { readonly kind: "askExactCount" }>;

const indexedRepositoryAnchorTimeoutMs = 10_000;

/** Read the same-run authoritative inventory total used to validate Ask. */
export async function loadIndexedRepositoryInventoryAnchor(
  apiBase: string,
  apiKey: string,
  fetcher: typeof fetch = fetch,
  timeoutMs = indexedRepositoryAnchorTimeoutMs,
): Promise<IndexedRepositoryInventoryAnchor> {
  const controller = new AbortController();
  const timeoutError = new Error(`indexed repository anchor timed out after ${timeoutMs} ms`);
  const timeout = setTimeout(() => controller.abort(timeoutError), timeoutMs);
  try {
    const headers: Record<string, string> = {};
    if (apiKey !== "") headers.Authorization = `Bearer ${apiKey}`;
    const response = await fetcher(`${apiBase}/api/v0/repositories?limit=1&offset=0`, {
      headers,
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`indexed repository anchor returned HTTP ${response.status}`);
    }
    const raw: unknown = await response.json();
    const root = asRecord(raw);
    const payload = Object.hasOwn(root, "data") ? asRecord(root.data) : root;
    const count = nonNegativeInteger(payload.count);
    const total = nonNegativeInteger(payload.total);
    if (count === null || total === null || total < count) {
      throw new Error("indexed repository anchor omitted a consistent count and total");
    }
    return { count, total };
  } catch (error) {
    if (controller.signal.aborted) throw timeoutError;
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

/** Return a bounded failure reason, or null when Ask matches the anchor. */
export function validateAskExactCountResponse(
  raw: unknown,
  anchor: IndexedRepositoryInventoryAnchor,
  expectedResultRef: string,
): string | null {
  const response = asRecord(raw);
  const result = asRecord(response.result);
  if (response.result_ref !== expectedResultRef) {
    return `result_ref ${String(response.result_ref)} did not match ${expectedResultRef}`;
  }
  if (result.total !== anchor.total) {
    return `Ask total ${String(result.total)} did not match same-run authoritative total ${anchor.total}`;
  }
  if (response.truth_class !== "deterministic") {
    return `truth_class ${String(response.truth_class)} was not deterministic`;
  }
  if (response.partial === true) {
    return "exact indexed-repository answer was marked partial";
  }
  const prose = typeof response.answer_prose === "string" ? response.answer_prose : "";
  if (!prose.includes(String(anchor.total)) || !prose.includes("list_indexed_repositories.total")) {
    return "answer prose did not name the same-run total and list_indexed_repositories.total";
  }
  const trace = Array.isArray(response.query_trace) ? response.query_trace : [];
  const authoritativeCall = trace
    .map(asRecord)
    .find(
      (entry) =>
        entry.tool === "list_indexed_repositories" &&
        entry.supported === true &&
        entry.truth_class === "deterministic",
    );
  if (!authoritativeCall) {
    return "query_trace omitted a supported deterministic list_indexed_repositories call";
  }
  return null;
}

/** Extract the terminal Ask answer from either JSON or SSE response bytes. */
export function parseAskAnswerPayload(body: string, contentType: string): unknown {
  if (!contentType.toLowerCase().includes("text/event-stream")) return JSON.parse(body);
  for (const block of body.split(/\r?\n\r?\n/)) {
    const lines = block.split(/\r?\n/);
    if (!lines.some((line) => line.trim() === "event: answer")) continue;
    const data = lines
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice("data:".length).trimStart())
      .join("\n");
    if (data.length > 0) return JSON.parse(data);
  }
  throw new Error("Ask SSE response omitted the terminal answer event");
}

/** Execute the retained browser proof for issue #5246. */
export async function executeAskExactCountWorkflow(
  page: Page,
  workflow: AskExactCountWorkflow,
  waitForQuiet: WaitForApiQuiet,
  anchor: IndexedRepositoryInventoryAnchor | null,
): Promise<RouteWorkflowObservation> {
  if (!anchor) {
    return failed(workflow.id, "same-run indexed repository inventory anchor is unavailable");
  }
  if (anchor.total > 1 && anchor.count === anchor.total) {
    return failed(
      workflow.id,
      "inventory anchor was not paginated; cannot disprove page-count substitution",
    );
  }

  const input = page.locator(workflow.fieldSelector);
  if ((await input.count()) !== 1) {
    return failed(workflow.id, `expected one ${workflow.fieldSelector} control`);
  }
  await input.fill(workflow.prompt);

  const submit = page.getByRole(workflow.role, { name: workflow.name, exact: true });
  const responsePromise = page.waitForResponse((candidate) =>
    matchesExpectedResponse(
      candidate,
      workflow.expectedRequestPath,
      "POST",
      workflow.acceptedResponseStatuses,
    ),
  );
  await submit.click();
  const response = await responsePromise;
  await waitForQuiet();

  const requestBody = asRecord(response.request().postDataJSON());
  if (requestBody.question !== workflow.prompt) {
    return failed(workflow.id, "Ask request did not preserve the exact issue prompt");
  }
  const validationFailure = validateAskExactCountResponse(
    parseAskAnswerPayload(await response.text(), response.headers()["content-type"] ?? ""),
    anchor,
    workflow.resultRef,
  );
  if (validationFailure) return failed(workflow.id, validationFailure);

  const outcomeCount = await visibleCount(page.locator(workflow.outcomeSelector));
  if (outcomeCount === 0) {
    return failed(workflow.id, `outcome did not render at ${workflow.outcomeSelector}`);
  }
  const forbidden = await forbiddenState(page, workflow);
  if (forbidden) return failed(workflow.id, forbidden);
  return passed(
    workflow.id,
    `matched same-run authoritative indexed repository total ${anchor.total}`,
    [dataShape(workflow.outcomeSelector, outcomeCount)],
    [requestObservation(response)],
  );
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return {};
  return value as Record<string, unknown>;
}

function nonNegativeInteger(value: unknown): number | null {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 ? value : null;
}
