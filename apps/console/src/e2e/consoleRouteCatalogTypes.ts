export interface ConsoleRoute {
  readonly path: string;
  readonly label: string;
  readonly area: RouteArea;
  readonly authMode?: ConsoleAuthMode;
  readonly productionPaths?: readonly string[];
  readonly workflow?: RouteWorkflowSpec;
}

export type ConsoleAuthMode = "bearer" | "browser_session";

export type WorkflowField =
  | {
      readonly requestKey?: string;
      readonly selector: string;
      readonly value: string;
      readonly valueEnv?: never;
    }
  | {
      readonly requestKey?: string;
      readonly selector: string;
      readonly value?: never;
      readonly valueEnv: string;
    };

export interface WorkflowEmptyState {
  readonly selector: string;
  readonly exactText: string;
}

export interface DeadCodeWorkflowControls {
  readonly applyName: string;
  readonly breakdownLinkSelector: string;
  readonly breakdownSelector: string;
  readonly breakdownToggleName: string;
  readonly countScopeSelector: string;
  readonly expectedCountScopeText: string;
  readonly languageOptionsSelector: string;
  readonly languageSelector: string;
  readonly observedLanguageSelector: string;
  readonly repositoryOptionsSelector: string;
  readonly repositorySelector: string;
  readonly resetKindName: string;
}

interface WorkflowResponseExpectationBase {
  readonly method: "GET" | "POST";
  readonly acceptedStatuses: readonly number[];
  readonly query?: Readonly<Record<string, string>>;
}

export type WorkflowResponseExpectation = WorkflowResponseExpectationBase &
  (
    | {
        readonly path: string;
        readonly pathPrefix?: never;
        readonly pathSuffix?: never;
      }
    | {
        readonly path?: never;
        readonly pathPrefix: string;
        readonly pathSuffix: string;
      }
  );

export interface WorkflowNonNetworkAuthority {
  readonly kind: "browser_session" | "static";
  readonly reason: string;
}

export interface WorkflowRecordedResponseOwnership {
  readonly requiredResponses?: readonly WorkflowResponseExpectation[];
  readonly requiredBootstrapResponses?: readonly WorkflowResponseExpectation[];
  readonly nonNetworkAuthority?: WorkflowNonNetworkAuthority;
}

export interface StateWorkflowResponseOwnership {
  readonly route?: readonly WorkflowResponseExpectation[];
  readonly bootstrap?: readonly WorkflowResponseExpectation[];
  readonly retainedDataRoute?: readonly WorkflowResponseExpectation[];
  readonly retainedDataBootstrap?: readonly WorkflowResponseExpectation[];
}

interface WorkflowGuards extends WorkflowRecordedResponseOwnership {
  // firstUsefulSelector is a route-owned surface present before an interaction
  // workflow runs. It prevents the performance harness from waiting for an
  // outcome that only the workflow itself can create.
  readonly firstUsefulSelector?: string;
  readonly forbiddenSelectors?: readonly string[];
  readonly forbiddenText?: string;
  readonly forbiddenTexts?: readonly string[];
}

export interface WorkflowTab extends WorkflowGuards {
  readonly name: string;
  readonly outcomeSelector: string;
}

export interface WorkflowFollowLink extends WorkflowGuards {
  readonly selector: string;
  readonly expectedPathPrefix: string;
  readonly expectedRequestPathPrefix: string;
  readonly expectedRequestMethod: "GET" | "POST";
  readonly acceptedResponseStatuses: readonly number[];
  readonly outcomeSelector: string;
}

export type RouteWorkflowSpec = WorkflowGuards &
  (
    | {
        readonly id: string;
        readonly kind: "state";
        readonly anySelectors: readonly string[];
        readonly emptyStates?: readonly WorkflowEmptyState[];
        readonly expectedPathPrefix?: string;
        readonly requiredResponses?: readonly WorkflowResponseExpectation[];
        readonly requiredBootstrapResponses?: readonly WorkflowResponseExpectation[];
        readonly retainedDataRequiredResponses?: readonly WorkflowResponseExpectation[];
        readonly retainedDataRequiredBootstrapResponses?: readonly WorkflowResponseExpectation[];
        readonly nonNetworkAuthority?: WorkflowNonNetworkAuthority;
      }
    | {
        readonly id: string;
        readonly kind: "fill";
        readonly selector: string;
        readonly value: string;
        readonly requestKey?: string;
        readonly outcomeSelector?: string;
        readonly outcomeTextIncludes?: string;
        readonly requireOutcomeChange?: boolean;
        readonly expectedRequestPath?: string;
        readonly expectedRequestMethod?: "GET" | "POST";
        readonly acceptedResponseStatuses?: readonly number[];
      }
    | {
        readonly id: string;
        readonly kind: "click";
        readonly role: "button" | "tab";
        readonly name: string;
        readonly outcomeSelector: string;
        readonly loadedStateSelector: string;
        readonly expectedRequestPath: string;
        readonly expectedRequestMethod: "GET" | "POST";
        readonly acceptedResponseStatuses: readonly number[];
      }
    | {
        readonly id: string;
        readonly kind: "submit";
        readonly fields: readonly WorkflowField[];
        readonly role: "button";
        readonly name: string;
        readonly scopeSelector?: string;
        readonly expectedRequestPath: string;
        readonly expectedRequestMethod: "GET" | "POST";
        readonly acceptedResponseStatuses: readonly number[];
        readonly additionalExpectedRequests?: readonly WorkflowResponseExpectation[];
        readonly expectedPagePath?: string;
        readonly outcomeSelector: string;
        readonly additionalOutcomeSelectors?: readonly string[];
      }
    | {
        readonly id: string;
        readonly kind: "askExactCount";
        readonly prompt: string;
        readonly fieldSelector: string;
        readonly role: "button";
        readonly name: string;
        readonly expectedRequestPath: string;
        readonly acceptedResponseStatuses: readonly number[];
        readonly outcomeSelector: string;
        readonly resultRef: string;
      }
    | {
        readonly id: string;
        readonly kind: "exactKind";
        readonly groupSelector: string;
        readonly preferredName: string;
        readonly outcomeCellSelector: string;
        readonly expectedRequestPath: string;
        readonly expectedRequestMethod: "POST";
        readonly acceptedResponseStatuses: readonly number[];
        readonly deadCodeControls?: DeadCodeWorkflowControls;
      }
    | {
        readonly id: string;
        readonly kind: "tabs";
        readonly proveVulnerabilityServiceTruth?: boolean;
        readonly tabs: readonly WorkflowTab[];
        readonly followLink?: WorkflowFollowLink;
      }
    | {
        readonly id: string;
        readonly kind: "repositoryDetails";
        readonly sourceLinkSelector: string;
        readonly sourceOutcomeSelector: string;
        readonly workspaceOutcomeSelector: string;
      }
  );

export type RouteArea =
  | "dashboard"
  | "repositories"
  | "service"
  | "graph"
  | "cloud"
  | "observability"
  | "operations"
  | "security"
  | "ask"
  | "system";

export interface NetworkAllowRule {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
  readonly reason: string;
}

export function liveState(
  id: string,
  anySelectors: readonly string[],
  forbiddenTexts: readonly string[] = [],
  emptyStates: readonly WorkflowEmptyState[] = [],
  responseOwnership: StateWorkflowResponseOwnership,
): RouteWorkflowSpec {
  return {
    id,
    kind: "state",
    anySelectors,
    forbiddenSelectors: [".src-err", ".async-guard-error"],
    forbiddenTexts,
    emptyStates,
    requiredResponses: responseOwnership.route,
    requiredBootstrapResponses: responseOwnership.bootstrap,
    retainedDataRequiredResponses: responseOwnership.retainedDataRoute,
    retainedDataRequiredBootstrapResponses: responseOwnership.retainedDataBootstrap,
  };
}
