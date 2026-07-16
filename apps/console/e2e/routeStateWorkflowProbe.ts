import type { Page } from "playwright";

import type {
  NetworkObservation,
  RouteWorkflowObservation,
  RouteWorkflowSpec,
} from "../src/e2e/routeAssertions.ts";
import {
  dataShape,
  failed,
  forbiddenState,
  passed,
  pathname,
  recordedResponseProof,
  visibleCount,
} from "./routeWorkflowProbeSupport.ts";

/** Prove one response-backed route state or its declared truthful empty state. */
export async function executeStateWorkflow(
  page: Page,
  workflow: Extract<RouteWorkflowSpec, { readonly kind: "state" }>,
  network: readonly NetworkObservation[],
  bootstrapNetwork: readonly NetworkObservation[],
): Promise<RouteWorkflowObservation> {
  const requiredResponses = workflow.requiredResponses ?? [];
  const requiredBootstrapResponses = workflow.requiredBootstrapResponses ?? [];
  const retainedDataRequiredResponses = workflow.retainedDataRequiredResponses ?? [];
  const retainedDataRequiredBootstrapResponses =
    workflow.retainedDataRequiredBootstrapResponses ?? [];
  if (
    requiredResponses.length === 0 &&
    requiredBootstrapResponses.length === 0 &&
    retainedDataRequiredResponses.length === 0 &&
    retainedDataRequiredBootstrapResponses.length === 0 &&
    workflow.nonNetworkAuthority === undefined
  ) {
    return failed(workflow.id, "state workflow has no production response ownership");
  }
  const routeProof = recordedResponseProof(network, requiredResponses, "route");
  if (!routeProof.ok) return failed(workflow.id, routeProof.detail);
  const bootstrapProof = recordedResponseProof(
    bootstrapNetwork,
    requiredBootstrapResponses,
    "bootstrap",
  );
  if (!bootstrapProof.ok) return failed(workflow.id, bootstrapProof.detail);
  const requests = [...routeProof.requests, ...bootstrapProof.requests];
  for (const selector of workflow.anySelectors) {
    const count = await visibleCount(page.locator(selector));
    if (count > 0) {
      const retainedDataProof = recordedResponseProof(
        network,
        retainedDataRequiredResponses,
        "route",
      );
      if (!retainedDataProof.ok) return failed(workflow.id, retainedDataProof.detail);
      const retainedDataBootstrapProof = recordedResponseProof(
        bootstrapNetwork,
        retainedDataRequiredBootstrapResponses,
        "bootstrap",
      );
      if (!retainedDataBootstrapProof.ok) {
        return failed(workflow.id, retainedDataBootstrapProof.detail);
      }
      if (workflow.expectedPathPrefix) {
        const currentPath = pathname(page.url());
        if (!currentPath.startsWith(workflow.expectedPathPrefix)) {
          return failed(
            workflow.id,
            `expected parameterized path prefix ${workflow.expectedPathPrefix}; found ${currentPath}`,
            [dataShape(selector, count)],
          );
        }
      }
      const forbidden = await forbiddenState(page, workflow);
      if (forbidden) return failed(workflow.id, forbidden, [dataShape(selector, count)]);
      return passed(
        workflow.id,
        `rendered response-backed visible ${selector}`,
        [dataShape(selector, count)],
        [...requests, ...retainedDataProof.requests, ...retainedDataBootstrapProof.requests],
      );
    }
  }
  for (const emptyState of workflow.emptyStates ?? []) {
    const locator = page.locator(emptyState.selector);
    const count = await visibleCount(locator);
    if (count === 0) continue;
    const texts = (await locator.allTextContents()).map((text) => text.trim());
    if (!texts.includes(emptyState.exactText)) continue;
    const forbidden = await forbiddenState(page, workflow);
    if (forbidden) {
      return failed(workflow.id, forbidden, [dataShape(emptyState.selector, count)]);
    }
    return passed(
      workflow.id,
      `rendered response-backed truthful empty state ${emptyState.exactText}`,
      [dataShape(emptyState.selector, count)],
      requests,
    );
  }
  return failed(
    workflow.id,
    `neither retained data nor a declared truthful empty state rendered visibly: ${workflow.anySelectors.join(", ")}`,
  );
}
