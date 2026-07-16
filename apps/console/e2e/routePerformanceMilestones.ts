import type { ConsoleRoute, RouteWorkflowSpec } from "../src/e2e/routeAssertions";

function selectorsForWorkflow(workflow: RouteWorkflowSpec): readonly string[] {
  if (workflow.firstUsefulSelector) return [workflow.firstUsefulSelector];
  switch (workflow.kind) {
    case "state":
      return [
        ...workflow.anySelectors,
        ...(workflow.emptyStates ?? []).map((emptyState) => emptyState.selector),
      ];
    case "fill":
      return [workflow.selector];
    case "click":
      return [workflow.loadedStateSelector];
    case "submit":
      return [workflow.scopeSelector ?? workflow.fields[0]?.selector ?? workflow.outcomeSelector];
    case "askExactCount":
      return [workflow.fieldSelector];
    case "exactKind":
      return [workflow.groupSelector];
    case "tabs":
      return workflow.tabs.slice(0, 1).map((tab) => tab.outcomeSelector);
    case "repositoryDetails":
      return [workflow.sourceLinkSelector];
  }
}

// firstUsefulSelectorForRoute identifies the earliest route-owned surface that
// lets an operator begin useful work. Shared shell/chrome selectors are
// deliberately excluded so the metric cannot go green while route content is
// still blank or loading.
export function firstUsefulSelectorForRoute(route: ConsoleRoute): string | null {
  if (route.workflow === undefined) return null;
  const selectors = selectorsForWorkflow(route.workflow).filter((selector) => selector !== "");
  return selectors.length === 0 ? null : selectors.join(", ");
}
