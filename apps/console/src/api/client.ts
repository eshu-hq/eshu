import type { EshuEnvelope, EshuError } from "./envelope";
import { EshuEnvelopeError, unwrapEnvelope } from "./envelope";

export const eshuEnvelopeAccept = "application/eshu.envelope+json";
export const browserSessionCSRFCookieName = "__Host-eshu_csrf";
export const browserSessionCSRFHeaderName = "X-Eshu-CSRF";

// EshuApiHttpError is thrown when the backend answers with a non-2xx status. It
// carries the numeric `status` so callers can branch on the HTTP code — e.g. the
// Explorer treats a 404 from code/relationships (a category mismatch, not a real
// failure) as an empty graph while still surfacing 5xx/timeout errors. See issue
// #1725.
export class EshuApiHttpError extends Error {
  readonly error: EshuError | null;
  readonly status: number;

  constructor(status: number, error: EshuError | null = null) {
    super(error ? `${error.code}: ${error.message}` : `Eshu API request failed with HTTP ${status}`);
    this.error = error;
    this.name = "EshuApiHttpError";
    this.status = status;
  }
}

// eshuDefaultTimeoutMs bounds every client request so one slow or hung backend
// endpoint (e.g. index-status under a corpus-scale regression) cannot block the
// console snapshot load forever and leave the app stuck "Connecting to the Eshu
// API…". See issue #1680.
export const eshuDefaultTimeoutMs = 15000;

export type EshuFetcher = (
  input: RequestInfo | URL,
  init?: RequestInit
) => Promise<Response>;

export interface EshuApiClientOptions {
  readonly apiKey?: string;
  readonly baseUrl: string;
  readonly fetcher?: EshuFetcher;
  // timeoutMs bounds each request via AbortSignal. A non-positive value disables
  // the timeout. Defaults to eshuDefaultTimeoutMs.
  readonly timeoutMs?: number;
}

export interface BrowserSessionAuth {
  readonly mode: "browser_session";
  readonly tenant_id?: string;
  readonly workspace_id?: string;
  readonly subject_class?: string;
  readonly subject_id_hash?: string;
  readonly policy_revision_hash?: string;
  readonly all_scopes: boolean;
  readonly allowed_scope_ids?: readonly string[];
  readonly allowed_repository_ids?: readonly string[];
}

export interface BrowserSessionResponse {
  readonly auth: BrowserSessionAuth;
  readonly csrf_token?: string;
  readonly idle_expires_at?: string;
  readonly absolute_expires_at?: string;
}

export class EshuApiClient {
  private readonly apiKey: string;
  private readonly baseUrl: string;
  private readonly fetcher: EshuFetcher;
  private readonly timeoutMs: number;

  constructor(options: EshuApiClientOptions) {
    this.apiKey = options.apiKey?.trim() ?? "";
    this.baseUrl = normalizeBaseUrl(options.baseUrl);
    this.fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
    this.timeoutMs = options.timeoutMs ?? eshuDefaultTimeoutMs;
  }

  async get<TData>(path: string): Promise<EshuEnvelope<TData>> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        credentials: "same-origin",
        headers: this.headers("GET"),
        signal
      }).then((response) => this.parse<TData>(response))
    );
  }

  async getJson<TData>(path: string): Promise<TData> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        credentials: "same-origin",
        headers: this.headers("GET"),
        signal
      }).then((response) => this.parseJson<TData>(response))
    );
  }

  async post<TData>(path: string, body: unknown): Promise<EshuEnvelope<TData>> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        body: JSON.stringify(body),
        credentials: "same-origin",
        headers: {
          ...this.headers("POST"),
          "Content-Type": "application/json"
        },
        method: "POST",
        signal
      }).then((response) => this.parse<TData>(response))
    );
  }

  async postJson<TData>(path: string, body: unknown): Promise<TData> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        body: JSON.stringify(body),
        credentials: "same-origin",
        headers: {
          ...this.headers("POST"),
          "Content-Type": "application/json"
        },
        method: "POST",
        signal
      }).then((response) => this.parseJson<TData>(response))
    );
  }

  async patchJson<TData>(path: string, body: unknown): Promise<TData> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        body: JSON.stringify(body),
        credentials: "same-origin",
        headers: {
          ...this.headers("PATCH"),
          "Content-Type": "application/json"
        },
        method: "PATCH",
        signal
      }).then((response) => this.parseJson<TData>(response))
    );
  }

  async delete(path: string): Promise<void> {
    return this.withTimeout((signal) =>
      this.fetcher(this.url(path), {
        credentials: "same-origin",
        headers: this.headers("DELETE"),
        method: "DELETE",
        signal
      }).then(async (response) => {
        if (!response.ok) {
          throw await this.httpError(response);
        }
      })
    );
  }

  async createBrowserSession(): Promise<BrowserSessionResponse> {
    return this.postJson<BrowserSessionResponse>("/api/v0/auth/browser-session", {});
  }

  async logoutBrowserSession(): Promise<void> {
    await this.delete("/api/v0/auth/browser-session");
  }

  async switchBrowserSessionContext(
    tenantId: string,
    workspaceId: string
  ): Promise<BrowserSessionResponse> {
    return this.patchJson<BrowserSessionResponse>(
      "/api/v0/auth/browser-session/context",
      { tenant_id: tenantId, workspace_id: workspaceId }
    );
  }

  // withTimeout runs a single request under a per-request abort deadline so one
  // slow or hung endpoint cannot block the console snapshot forever (issue
  // #1680). It uses an AbortController from the ambient realm (not
  // AbortSignal.timeout) so the signal is the same AbortSignal type the fetch
  // and Request implementations validate against, and always clears the timer so
  // no pending timeout leaks once the request settles. A non-positive or
  // non-finite timeout disables the deadline and passes signal: undefined.
  private async withTimeout<T>(
    run: (signal: AbortSignal | undefined) => Promise<T>
  ): Promise<T> {
    if (!Number.isFinite(this.timeoutMs) || this.timeoutMs <= 0) {
      return run(undefined);
    }
    const controller = new AbortController();
    const timer = setTimeout(() => {
      controller.abort(
        new DOMException(
          `Eshu API request timed out after ${this.timeoutMs}ms`,
          "TimeoutError"
        )
      );
    }, this.timeoutMs);
    try {
      return await run(controller.signal);
    } finally {
      clearTimeout(timer);
    }
  }

  private url(path: string): string {
    const cleanPath = path.startsWith("/") ? path.slice(1) : path;
    return new URL(cleanPath, absoluteBaseUrl(this.baseUrl)).toString();
  }

  private headers(method: string): HeadersInit {
    if (this.apiKey.length === 0) {
      return cookieSessionHeaders(method);
    }
    return {
      ...envelopeHeaders(),
      Authorization: `Bearer ${this.apiKey}`
    };
  }

  private async parse<TData>(response: Response): Promise<EshuEnvelope<TData>> {
    if (!response.ok) {
      throw await this.httpError(response);
    }
    const parsed = (await response.json()) as EshuEnvelope<TData>;
    return parsed;
  }

  private async parseJson<TData>(response: Response): Promise<TData> {
    if (!response.ok) {
      throw await this.httpError(response);
    }
    const parsed = (await response.json()) as unknown;
    if (isEnvelope(parsed)) {
      return unwrapEnvelope(parsed as EshuEnvelope<TData>).data;
    }
    return parsed as TData;
  }

  private async httpError(response: Response): Promise<EshuApiHttpError> {
    try {
      const parsed = (await response.clone().json()) as unknown;
      if (isEnvelope(parsed)) {
        const envelope = parsed as EshuEnvelope<unknown>;
        if (envelope.error !== null) {
          return new EshuApiHttpError(response.status, envelope.error);
        }
      }
    } catch {
      // Non-JSON or malformed error responses still carry the HTTP status.
    }
    return new EshuApiHttpError(response.status);
  }
}

function normalizeBaseUrl(baseUrl: string): string {
  const trimmed = baseUrl.trim();
  if (trimmed.length === 0) {
    throw new Error("Eshu API base URL is required");
  }
  return trimmed.endsWith("/") ? trimmed : `${trimmed}/`;
}

function absoluteBaseUrl(baseUrl: string): string {
  if (baseUrl.startsWith("http://") || baseUrl.startsWith("https://")) {
    return baseUrl;
  }
  const origin = globalThis.location?.origin ?? "http://localhost";
  return new URL(baseUrl, origin).toString();
}

function isEnvelope(value: unknown): boolean {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Partial<EshuEnvelope<unknown>>;
  if (candidate.error !== undefined && candidate.error !== null) {
    const error = candidate.error;
    if (typeof error.code !== "string" || typeof error.message !== "string") {
      throw new EshuEnvelopeError({
        code: "invalid_error_envelope",
        message: "Eshu API returned an invalid error envelope"
      });
    }
  }
  return "data" in value || "truth" in value || "error" in value;
}

function envelopeHeaders(): HeadersInit {
  return {
    Accept: eshuEnvelopeAccept
  };
}

function cookieSessionHeaders(method: string): HeadersInit {
  const headers: Record<string, string> = {
    Accept: eshuEnvelopeAccept
  };
  if (requiresCSRF(method)) {
    const csrfToken = readCookie(browserSessionCSRFCookieName);
    if (csrfToken.length > 0) {
      headers[browserSessionCSRFHeaderName] = csrfToken;
    }
  }
  return headers;
}

function requiresCSRF(method: string): boolean {
  switch (method.trim().toUpperCase()) {
    case "GET":
    case "HEAD":
    case "OPTIONS":
    case "TRACE":
      return false;
    default:
      return true;
  }
}

function readCookie(name: string): string {
  const cookieHeader = globalThis.document?.cookie ?? "";
  for (const part of cookieHeader.split(";")) {
    const [rawName, ...rawValue] = part.trim().split("=");
    if (rawName === name) {
      return decodeURIComponent(rawValue.join("="));
    }
  }
  return "";
}
