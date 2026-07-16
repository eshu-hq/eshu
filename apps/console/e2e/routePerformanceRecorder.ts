import type { RecordedRouteRequest } from "./routePerformanceDiagnostics";

export interface DevToolsSession {
  send(method: string): Promise<unknown>;
  on(event: string, listener: (payload: unknown) => void): void;
  off(event: string, listener: (payload: unknown) => void): void;
}

export interface RoutePerformanceCapture {
  readonly firstSequence: number;
  readonly startedAtMs: number;
}

export interface RoutePerformanceRecorder {
  beginCapture(): RoutePerformanceCapture;
  recordsSince(capture: RoutePerformanceCapture): readonly RecordedRouteRequest[];
  stop(): void;
}

interface PendingRequest {
  readonly sequence: number;
  readonly method: string;
  readonly url: string;
  readonly monotonicStartedAtMs: number;
  readonly wallStartedAtMs: number;
  responseReceivedMonotonicMs: number | null;
  status: number;
}

interface RequestWillBeSentEvent {
  readonly requestId: string;
  readonly timestamp: number;
  readonly wallTime: number;
  readonly request: {
    readonly method: string;
    readonly url: string;
  };
}

interface ResponseReceivedEvent {
  readonly requestId: string;
  readonly timestamp: number;
  readonly response: { readonly status: number };
}

interface LoadingFinishedEvent {
  readonly requestId: string;
  readonly timestamp: number;
  readonly encodedDataLength: number;
}

interface LoadingFailedEvent {
  readonly requestId: string;
  readonly timestamp: number;
}

function isRecord(payload: unknown): payload is Record<string, unknown> {
  return typeof payload === "object" && payload !== null;
}

function requestWillBeSentEvent(payload: unknown): RequestWillBeSentEvent | null {
  if (!isRecord(payload) || !isRecord(payload.request)) return null;
  const { requestId, timestamp, wallTime, request } = payload;
  if (
    typeof requestId !== "string" ||
    typeof timestamp !== "number" ||
    typeof wallTime !== "number" ||
    typeof request.method !== "string" ||
    typeof request.url !== "string"
  ) {
    return null;
  }
  return { requestId, timestamp, wallTime, request: { method: request.method, url: request.url } };
}

function responseReceivedEvent(payload: unknown): ResponseReceivedEvent | null {
  if (!isRecord(payload) || !isRecord(payload.response)) return null;
  if (
    typeof payload.requestId !== "string" ||
    typeof payload.timestamp !== "number" ||
    typeof payload.response.status !== "number"
  ) {
    return null;
  }
  return {
    requestId: payload.requestId,
    timestamp: payload.timestamp,
    response: { status: payload.response.status },
  };
}

function loadingFinishedEvent(payload: unknown): LoadingFinishedEvent | null {
  if (!isRecord(payload)) return null;
  if (
    typeof payload.requestId !== "string" ||
    typeof payload.timestamp !== "number" ||
    typeof payload.encodedDataLength !== "number"
  ) {
    return null;
  }
  return {
    requestId: payload.requestId,
    timestamp: payload.timestamp,
    encodedDataLength: payload.encodedDataLength,
  };
}

function loadingFailedEvent(payload: unknown): LoadingFailedEvent | null {
  if (!isRecord(payload)) return null;
  if (typeof payload.requestId !== "string" || typeof payload.timestamp !== "number") return null;
  return { requestId: payload.requestId, timestamp: payload.timestamp };
}

function completedRequest(
  pending: PendingRequest,
  finishedMonotonicMs: number,
  transferredBytes: number,
  failed: boolean,
): RecordedRouteRequest {
  const durationMs = Math.max(0, Math.round(finishedMonotonicMs - pending.monotonicStartedAtMs));
  return {
    sequence: pending.sequence,
    method: pending.method,
    url: pending.url,
    status: pending.status,
    startedAtMs: pending.wallStartedAtMs,
    responseReceivedAtMs:
      pending.responseReceivedMonotonicMs === null
        ? null
        : pending.wallStartedAtMs +
          Math.max(
            0,
            Math.round(pending.responseReceivedMonotonicMs - pending.monotonicStartedAtMs),
          ),
    finishedAtMs: pending.wallStartedAtMs + durationMs,
    transferredBytes: Math.max(0, Math.round(transferredBytes)),
    failed,
  };
}

export async function installRoutePerformanceRecorder(
  session: DevToolsSession,
  now: () => number = Date.now,
): Promise<RoutePerformanceRecorder> {
  const pending = new Map<string, PendingRequest>();
  const completed: RecordedRouteRequest[] = [];
  let nextSequence = 1;

  const onRequestWillBeSent = (payload: unknown): void => {
    const event = requestWillBeSentEvent(payload);
    if (event === null || !event.request.url.includes("/eshu-api/")) return;
    pending.set(event.requestId, {
      sequence: nextSequence,
      method: event.request.method,
      url: event.request.url,
      monotonicStartedAtMs: event.timestamp * 1_000,
      wallStartedAtMs: event.wallTime * 1_000,
      responseReceivedMonotonicMs: null,
      status: 0,
    });
    nextSequence += 1;
  };
  const onResponseReceived = (payload: unknown): void => {
    const event = responseReceivedEvent(payload);
    if (event === null) return;
    const request = pending.get(event.requestId);
    if (request !== undefined) {
      request.status = event.response.status;
      request.responseReceivedMonotonicMs = event.timestamp * 1_000;
    }
  };
  const onLoadingFinished = (payload: unknown): void => {
    const event = loadingFinishedEvent(payload);
    if (event === null) return;
    const request = pending.get(event.requestId);
    if (request === undefined) return;
    completed.push(
      completedRequest(request, event.timestamp * 1_000, event.encodedDataLength, false),
    );
    pending.delete(event.requestId);
  };
  const onLoadingFailed = (payload: unknown): void => {
    const event = loadingFailedEvent(payload);
    if (event === null) return;
    const request = pending.get(event.requestId);
    if (request === undefined) return;
    completed.push(completedRequest(request, event.timestamp * 1_000, 0, true));
    pending.delete(event.requestId);
  };

  session.on("Network.requestWillBeSent", onRequestWillBeSent);
  session.on("Network.responseReceived", onResponseReceived);
  session.on("Network.loadingFinished", onLoadingFinished);
  session.on("Network.loadingFailed", onLoadingFailed);
  await session.send("Network.enable");

  return {
    beginCapture: () => ({ firstSequence: nextSequence, startedAtMs: now() }),
    recordsSince: (capture) =>
      completed
        .filter((request) => request.sequence >= capture.firstSequence)
        .map((request) => ({
          ...request,
          startedAtMs: Math.max(0, Math.round(request.startedAtMs - capture.startedAtMs)),
          responseReceivedAtMs:
            request.responseReceivedAtMs === null
              ? null
              : Math.max(0, Math.round(request.responseReceivedAtMs - capture.startedAtMs)),
          finishedAtMs: Math.max(0, Math.round(request.finishedAtMs - capture.startedAtMs)),
        })),
    stop: () => {
      session.off("Network.requestWillBeSent", onRequestWillBeSent);
      session.off("Network.responseReceived", onResponseReceived);
      session.off("Network.loadingFinished", onLoadingFinished);
      session.off("Network.loadingFailed", onLoadingFailed);
    },
  };
}
