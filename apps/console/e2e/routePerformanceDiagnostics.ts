import { safeReportPathname } from "../src/e2e/routeAssertions";

export interface RecordedRouteRequest {
  readonly sequence: number;
  readonly method: string;
  readonly url: string;
  readonly status: number;
  readonly startedAtMs: number;
  readonly responseReceivedAtMs: number | null;
  readonly finishedAtMs: number;
  readonly transferredBytes: number;
  readonly failed: boolean;
}

interface RequestShape {
  readonly method: string;
  readonly pathname: string;
  readonly queryKeys: readonly string[];
}

export interface RouteRequestTimelineEntry extends RequestShape {
  readonly sequence: number;
  readonly startedAtMs: number;
  readonly durationMs: number;
  readonly timeToFirstByteMs: number | null;
  readonly downloadMs: number | null;
  readonly status: number;
  readonly transferredBytes: number;
  readonly failed: boolean;
  readonly predecessorSequence: number | null;
  readonly overlappingSequences: readonly number[];
}

export interface DuplicateRouteRequest extends RequestShape {
  readonly count: number;
  readonly sequences: readonly number[];
}

export interface RoutePerformanceMilestones {
  readonly firstUsefulContentMs: number;
  readonly routeReadyMs: number;
}

export interface RoutePerformanceDiagnostics extends RoutePerformanceMilestones {
  readonly requestCount: number;
  readonly duplicateRequestCount: number;
  readonly duplicateRequests: readonly DuplicateRouteRequest[];
  readonly transferredBytes: number;
  readonly maxSimultaneousRequests: number;
  readonly postResponseToFirstUsefulContentMs: number | null;
  readonly slowestRequest: RouteRequestTimelineEntry | null;
  readonly timeline: readonly RouteRequestTimelineEntry[];
}

function requestShape(request: RecordedRouteRequest): RequestShape {
  try {
    const parsed = new URL(request.url);
    return {
      method: request.method.toUpperCase(),
      pathname: safeReportPathname(request.url),
      queryKeys: [...new Set(parsed.searchParams.keys())].sort(),
    };
  } catch {
    return {
      method: request.method.toUpperCase(),
      pathname: "invalid-url",
      queryKeys: [],
    };
  }
}

function predecessorSequence(
  request: RecordedRouteRequest,
  requests: readonly RecordedRouteRequest[],
): number | null {
  const predecessors = requests.filter(
    (candidate) =>
      candidate.sequence !== request.sequence && candidate.finishedAtMs <= request.startedAtMs,
  );
  predecessors.sort(
    (left, right) => right.finishedAtMs - left.finishedAtMs || right.sequence - left.sequence,
  );
  return predecessors[0]?.sequence ?? null;
}

function overlappingSequences(
  request: RecordedRouteRequest,
  requests: readonly RecordedRouteRequest[],
): readonly number[] {
  return requests
    .filter(
      (candidate) =>
        candidate.sequence !== request.sequence &&
        candidate.startedAtMs <= request.startedAtMs &&
        candidate.finishedAtMs > request.startedAtMs &&
        (candidate.startedAtMs < request.startedAtMs || candidate.sequence < request.sequence),
    )
    .map((candidate) => candidate.sequence)
    .sort((left, right) => left - right);
}

function maximumConcurrency(requests: readonly RecordedRouteRequest[]): number {
  let maximum = 0;
  for (const request of requests) {
    const active = requests.filter(
      (candidate) =>
        candidate.startedAtMs <= request.startedAtMs &&
        candidate.finishedAtMs > request.startedAtMs,
    ).length;
    maximum = Math.max(maximum, active);
  }
  return maximum;
}

function duplicateRequests(
  requests: readonly RecordedRouteRequest[],
): readonly DuplicateRouteRequest[] {
  const groups = new Map<string, RecordedRouteRequest[]>();
  for (const request of requests) {
    const key = `${request.method.toUpperCase()} ${request.url}`;
    const group = groups.get(key) ?? [];
    group.push(request);
    groups.set(key, group);
  }
  return [...groups.values()]
    .filter((group) => group.length > 1)
    .sort((left, right) => left[0].sequence - right[0].sequence)
    .map((group) => ({
      ...requestShape(group[0]),
      count: group.length,
      sequences: group.map((request) => request.sequence).sort((left, right) => left - right),
    }));
}

export function summarizeRoutePerformance(
  requests: readonly RecordedRouteRequest[],
  milestones: RoutePerformanceMilestones,
): RoutePerformanceDiagnostics {
  const ordered = [...requests].sort(
    (left, right) => left.startedAtMs - right.startedAtMs || left.sequence - right.sequence,
  );
  const timeline = ordered.map<RouteRequestTimelineEntry>((request) => ({
    ...requestShape(request),
    sequence: request.sequence,
    startedAtMs: request.startedAtMs,
    durationMs: Math.max(0, request.finishedAtMs - request.startedAtMs),
    timeToFirstByteMs:
      request.responseReceivedAtMs === null
        ? null
        : Math.max(0, request.responseReceivedAtMs - request.startedAtMs),
    downloadMs:
      request.responseReceivedAtMs === null
        ? null
        : Math.max(0, request.finishedAtMs - request.responseReceivedAtMs),
    status: request.status,
    transferredBytes: Math.max(0, request.transferredBytes),
    failed: request.failed,
    predecessorSequence: predecessorSequence(request, ordered),
    overlappingSequences: overlappingSequences(request, ordered),
  }));
  const duplicates = duplicateRequests(ordered);
  const responsesBeforeUsefulContent = ordered.filter(
    (request) => request.finishedAtMs <= milestones.firstUsefulContentMs,
  );
  const lastResponseBeforeUsefulContent = responsesBeforeUsefulContent.reduce<number | null>(
    (latest, request) => Math.max(latest ?? request.finishedAtMs, request.finishedAtMs),
    null,
  );
  const slowestRequest = timeline.reduce<RouteRequestTimelineEntry | null>(
    (slowest, request) =>
      slowest === null || request.durationMs > slowest.durationMs ? request : slowest,
    null,
  );
  return {
    ...milestones,
    requestCount: timeline.length,
    duplicateRequestCount: duplicates.reduce((count, group) => count + group.count - 1, 0),
    duplicateRequests: duplicates,
    transferredBytes: timeline.reduce((total, request) => total + request.transferredBytes, 0),
    maxSimultaneousRequests: maximumConcurrency(ordered),
    postResponseToFirstUsefulContentMs:
      lastResponseBeforeUsefulContent === null
        ? null
        : Math.max(0, milestones.firstUsefulContentMs - lastResponseBeforeUsefulContent),
    slowestRequest,
    timeline,
  };
}
