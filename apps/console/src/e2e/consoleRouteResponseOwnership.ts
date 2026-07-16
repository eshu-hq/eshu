import type {
  StateWorkflowResponseOwnership,
  WorkflowResponseExpectation,
} from "./consoleRouteCatalogTypes";

type ResponseQuery = Readonly<Record<string, string>>;

export function getResponse(path: string, query?: ResponseQuery): WorkflowResponseExpectation {
  return { path, method: "GET", acceptedStatuses: [200], query };
}

export function postResponse(path: string): WorkflowResponseExpectation {
  return { path, method: "POST", acceptedStatuses: [200] };
}

export function boundedGetResponse(
  pathPrefix: string,
  pathSuffix: string,
): WorkflowResponseExpectation {
  return { pathPrefix, pathSuffix, method: "GET", acceptedStatuses: [200] };
}

export function routeOwnership(
  ...route: readonly WorkflowResponseExpectation[]
): StateWorkflowResponseOwnership {
  return { route };
}

export function bootstrapOwnership(
  ...bootstrap: readonly WorkflowResponseExpectation[]
): StateWorkflowResponseOwnership {
  return { bootstrap };
}
