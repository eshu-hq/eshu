// askEshu.ts — typed client for the natural-language POST /api/v0/ask surface.
//
// The endpoint is content-negotiated on a single route:
//   Accept: text/event-stream  -> SSE: trace* , answer , error , done
//   Accept: application/json    -> 200 answer JSON (sync)
// It requires a shared/admin token (scoped tokens receive 403) and is
// default-off (503 with {state:"unavailable", reason} when ESHU_ASK_ENABLED is
// unset or no provider profile is configured). This module speaks that contract
// over raw fetch so the console can stream reasoning steps live, fall back to a
// single synchronous request, and surface the disabled/forbidden states
// cleanly. It never renders or logs raw provider bodies — only the bounded
// fields below. See docs/public/reference/http-api.md and
// go/internal/query/openapi_paths_ask.go (the source of truth for the shape).

import type { EshuFetcher } from "./client";
import { eshuEnvelopeAccept } from "./client";
import {
  narrationData,
  narrationState,
  normalizeAnswer,
  normalizeStreamError,
  normalizeTraceStep
} from "./askEshuNormalize";

/** Requested answer format. `auto` lets the engine infer from the question. */
export type AskFormat = "auto" | "markdown" | "mermaid" | "json" | "yaml" | "csv";

/** Truth classification carried by an answer or a single trace step. */
export type AskTruthClass =
  | "deterministic"
  | "derived"
  | "fallback"
  | "semantic_observation"
  | "code_hint"
  | "unsupported";

/** A rendered output artifact with per-format validation notes. */
export interface AskArtifact {
  readonly format: string;
  readonly content: string;
  readonly issues: readonly string[];
}

/** One tool call in the agent loop, in invocation order. */
export interface AskTraceStep {
  readonly tool: string;
  readonly args?: Record<string, unknown>;
  readonly supported?: boolean;
  readonly truth_class?: string;
  readonly err?: string;
}

// AskEvidenceHandle is an addressable evidence handle. The wire shape is loosely
// typed on the backend, so the console reads the bounded fields it understands
// and preserves the rest without inventing structure.
export interface AskEvidenceHandle {
  readonly kind?: string;
  readonly label?: string;
  readonly ref?: string;
  readonly relative_path?: string;
  readonly start_line?: number;
  readonly [key: string]: unknown;
}

/** Full answer packet from POST /api/v0/ask, normalized for the UI. */
export interface AskAnswer {
  readonly answer_prose: string;
  readonly artifacts: readonly AskArtifact[];
  readonly truth_class: AskTruthClass;
  readonly query_trace: readonly AskTraceStep[];
  readonly partial: boolean;
  readonly limitations: readonly string[];
  readonly evidence_handles: readonly AskEvidenceHandle[];
}

/** Terminal error states the Ask surface can present. */
export type AskErrorState = "forbidden" | "unavailable" | "bad_request" | "error";

/** Bounded error payload surfaced to the user (never a raw response body). */
export interface AskError {
  readonly state: AskErrorState;
  readonly reason: string;
}

/** Answer-narration capability states from GET /api/v0/status/answer-narration. */
export type AskNarrationState = "available" | "unavailable" | "disabled";

/** Capability probe result deciding the disabled vs evidence-only presentation. */
export interface AskNarrationProbe {
  readonly state: AskNarrationState;
  readonly reason: string;
}

/** Connection coordinates for the live Eshu API. */
export interface AskConnection {
  readonly baseUrl: string;
  readonly apiKey: string;
  // fetcher is injectable for tests; defaults to the global fetch.
  readonly fetcher?: EshuFetcher;
}

/** Streaming/sync callbacks plus cancellation for a single ask run. */
export interface AskRunOptions {
  readonly connection: AskConnection;
  readonly question: string;
  readonly format: AskFormat;
  // stream defaults to true (SSE). Pass false to use the synchronous JSON path.
  readonly stream?: boolean;
  readonly signal?: AbortSignal;
  readonly onTrace?: (step: AskTraceStep) => void;
  readonly onAnswer?: (answer: AskAnswer) => void;
  readonly onError?: (error: AskError) => void;
  readonly onDone?: () => void;
  readonly onAbort?: () => void;
}

const askPath = "/api/v0/ask";
const narrationStatusPath = "/api/v0/status/answer-narration";

interface AskHandlers {
  readonly onTrace: (step: AskTraceStep) => void;
  readonly onAnswer: (answer: AskAnswer) => void;
  readonly onError: (error: AskError) => void;
  readonly onDone: () => void;
  readonly onAbort: () => void;
}

// askEshu runs one question against POST /api/v0/ask. Streaming is the default
// so reasoning steps appear live; pass stream:false for the synchronous path.
// Cancellation flows through options.signal; on abort onAbort fires and no error
// is surfaced. The terminal onDone always fires exactly once.
export function askEshu(options: AskRunOptions): void {
  const handlers: AskHandlers = {
    onTrace: options.onTrace ?? noop,
    onAnswer: options.onAnswer ?? noop,
    onError: options.onError ?? noop,
    onDone: once(options.onDone ?? noop),
    onAbort: options.onAbort ?? noop
  };
  const body = JSON.stringify({ question: options.question, format: options.format });
  if (options.stream === false) {
    void runSync(options.connection, body, handlers, options.signal);
    return;
  }
  void runStream(options.connection, body, handlers, options.signal);
}

// askNarrationStatus probes the answer-narration capability. It maps a failed
// probe to an `unavailable` state so the page degrades to an evidence-only hint
// rather than presenting a broken control. Never throws.
export async function askNarrationStatus(
  connection: AskConnection,
  signal?: AbortSignal
): Promise<AskNarrationProbe> {
  const fetcher = connection.fetcher ?? globalFetch();
  try {
    const response = await fetcher(joinUrl(connection.baseUrl, narrationStatusPath), {
      headers: getHeaders(connection.apiKey),
      signal
    });
    if (!response.ok) {
      return { state: "unavailable", reason: `probe failed: HTTP ${response.status}` };
    }
    const parsed = (await response.json()) as unknown;
    const data = narrationData(parsed);
    const state = narrationState(data.state);
    return { state, reason: typeof data.reason === "string" ? data.reason : "" };
  } catch (error) {
    return { state: "unavailable", reason: errorMessage(error) || "probe failed" };
  }
}

async function runStream(
  connection: AskConnection,
  body: string,
  handlers: AskHandlers,
  signal?: AbortSignal
): Promise<void> {
  const fetcher = connection.fetcher ?? globalFetch();
  try {
    const response = await fetcher(joinUrl(connection.baseUrl, askPath), {
      method: "POST",
      headers: postHeaders(connection.apiKey, "text/event-stream"),
      body,
      signal
    });
    if (handleStatus(response, handlers)) {
      return;
    }
    if (!response.body) {
      // No readable stream (e.g. a buffered fetch polyfill): retry the sync path.
      await consumeSyncResponse(response, handlers);
      return;
    }
    await consumeEventStream(response.body, handlers);
    handlers.onDone();
  } catch (error) {
    finishWithError(error, handlers);
  }
}

async function runSync(
  connection: AskConnection,
  body: string,
  handlers: AskHandlers,
  signal?: AbortSignal
): Promise<void> {
  const fetcher = connection.fetcher ?? globalFetch();
  try {
    const response = await fetcher(joinUrl(connection.baseUrl, askPath), {
      method: "POST",
      headers: postHeaders(connection.apiKey, "application/json"),
      body,
      signal
    });
    if (handleStatus(response, handlers)) {
      return;
    }
    await consumeSyncResponse(response, handlers);
  } catch (error) {
    finishWithError(error, handlers);
  }
}

async function consumeSyncResponse(response: Response, handlers: AskHandlers): Promise<void> {
  const raw = (await response.json()) as unknown;
  const answer = normalizeAnswer(raw);
  for (const step of answer.query_trace) {
    handlers.onTrace(step);
  }
  handlers.onAnswer(answer);
  handlers.onDone();
}

async function consumeEventStream(
  stream: ReadableStream<Uint8Array>,
  handlers: AskHandlers
): Promise<void> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const chunk = await reader.read();
    if (chunk.done) {
      break;
    }
    buffer += decoder.decode(chunk.value, { stream: true });
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      dispatchEvent(buffer.slice(0, boundary), handlers);
      buffer = buffer.slice(boundary + 2);
      boundary = buffer.indexOf("\n\n");
    }
  }
}

function dispatchEvent(raw: string, handlers: AskHandlers): void {
  let event = "message";
  let data = "";
  for (const line of raw.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      // Per the SSE spec, multiple data: lines join with "\n" and only a single
      // leading space after the colon is stripped (trailing whitespace, which
      // could be significant inside a JSON string, is preserved).
      const value = line.slice(5);
      data += (data.length > 0 ? "\n" : "") + (value.startsWith(" ") ? value.slice(1) : value);
    }
  }
  if (data.length === 0) {
    return;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return;
  }
  if (event === "trace") {
    handlers.onTrace(normalizeTraceStep(parsed));
  } else if (event === "answer") {
    handlers.onAnswer(normalizeAnswer(parsed));
  } else if (event === "error") {
    handlers.onError(normalizeStreamError(parsed));
  }
  // The `done` event is the stream terminator; onDone is fired by the reader
  // loop once the body closes, so it is intentionally ignored here.
}

// handleStatus surfaces the non-2xx contract states. Returns true when it has
// terminated the run (so the caller stops), false to continue with the body.
function handleStatus(response: Response, handlers: AskHandlers): boolean {
  if (response.ok) {
    return false;
  }
  if (response.status === 403) {
    handlers.onError({
      state: "forbidden",
      reason: "This token is scoped. Ask Eshu requires a shared or admin token."
    });
    handlers.onDone();
    return true;
  }
  if (response.status === 503) {
    void respondUnavailable(response, handlers);
    return true;
  }
  if (response.status === 400) {
    handlers.onError({ state: "bad_request", reason: "Type a question before submitting." });
    handlers.onDone();
    return true;
  }
  handlers.onError({ state: "error", reason: `Ask Eshu request failed (HTTP ${response.status}).` });
  handlers.onDone();
  return true;
}

async function respondUnavailable(response: Response, handlers: AskHandlers): Promise<void> {
  let reason = "Ask Eshu is disabled on this deployment.";
  try {
    const parsed = (await response.json()) as { readonly reason?: unknown };
    if (typeof parsed.reason === "string" && parsed.reason.length > 0) {
      reason = parsed.reason;
    }
  } catch {
    // Keep the default reason when the body is missing or not JSON.
  }
  handlers.onError({ state: "unavailable", reason });
  handlers.onDone();
}

function finishWithError(error: unknown, handlers: AskHandlers): void {
  if (isAbortError(error)) {
    // A cancelled run is not a completion: fire onAbort only so the caller
    // returns to idle rather than transitioning to a "done" state.
    handlers.onAbort();
    return;
  }
  handlers.onError({ state: "error", reason: errorMessage(error) || "Could not reach Ask Eshu." });
  handlers.onDone();
}

function postHeaders(apiKey: string, accept: string): HeadersInit {
  return { ...getHeaders(apiKey), Accept: accept, "Content-Type": "application/json" };
}

function getHeaders(apiKey: string): Record<string, string> {
  const headers: Record<string, string> = { Accept: eshuEnvelopeAccept };
  const key = apiKey.trim();
  if (key.length > 0) {
    headers.Authorization = `Bearer ${key}`;
  }
  return headers;
}

function joinUrl(baseUrl: string, path: string): string {
  const base = baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
  const cleanPath = path.startsWith("/") ? path.slice(1) : path;
  const origin = globalThis.location?.origin ?? "http://localhost";
  const absoluteBase =
    base.startsWith("http://") || base.startsWith("https://") ? base : new URL(base, origin).toString();
  return new URL(cleanPath, absoluteBase).toString();
}

// isAbortError recognizes both Error and DOMException aborts. Browsers reject an
// aborted fetch with a DOMException named "AbortError", which is NOT an instance
// of Error, so the name is checked structurally rather than via instanceof.
function isAbortError(error: unknown): boolean {
  return hasName(error) && error.name === "AbortError";
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "object" && error !== null && typeof (error as { message?: unknown }).message === "string") {
    return (error as { message: string }).message;
  }
  return "";
}

function hasName(error: unknown): error is { name: string } {
  return typeof error === "object" && error !== null && typeof (error as { name?: unknown }).name === "string";
}

function globalFetch(): EshuFetcher {
  return globalThis.fetch.bind(globalThis);
}

function noop(): void {
  // no-op default callback
}

function once(fn: () => void): () => void {
  let called = false;
  return () => {
    if (called) {
      return;
    }
    called = true;
    fn();
  };
}
