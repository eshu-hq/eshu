import type { EshuEnvelope } from "./envelope";
import { EshuEnvelopeError, unwrapEnvelope } from "./envelope";

export const eshuEnvelopeAccept = "application/eshu.envelope+json";

export type EshuFetcher = (
  input: RequestInfo | URL,
  init?: RequestInit
) => Promise<Response>;

export interface EshuApiClientOptions {
  readonly apiKey?: string;
  readonly baseUrl: string;
  readonly fetcher?: EshuFetcher;
}

export class EshuApiClient {
  private readonly apiKey: string;
  private readonly baseUrl: string;
  private readonly fetcher: EshuFetcher;

  constructor(options: EshuApiClientOptions) {
    this.apiKey = options.apiKey?.trim() ?? "";
    this.baseUrl = normalizeBaseUrl(options.baseUrl);
    this.fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  }

  async get<TData>(path: string): Promise<EshuEnvelope<TData>> {
    const response = await this.fetcher(this.url(path), {
      headers: this.headers()
    });
    return this.parse<TData>(response);
  }

  async getJson<TData>(path: string): Promise<TData> {
    const response = await this.fetcher(this.url(path), {
      headers: this.headers()
    });
    return this.parseJson<TData>(response);
  }

  async post<TData>(path: string, body: unknown): Promise<EshuEnvelope<TData>> {
    const response = await this.fetcher(this.url(path), {
      body: JSON.stringify(body),
      headers: {
        ...this.headers(),
        "Content-Type": "application/json"
      },
      method: "POST"
    });
    return this.parse<TData>(response);
  }

  async postJson<TData>(path: string, body: unknown): Promise<TData> {
    const response = await this.fetcher(this.url(path), {
      body: JSON.stringify(body),
      headers: {
        ...this.headers(),
        "Content-Type": "application/json"
      },
      method: "POST"
    });
    return this.parseJson<TData>(response);
  }

  private url(path: string): string {
    const cleanPath = path.startsWith("/") ? path.slice(1) : path;
    return new URL(cleanPath, absoluteBaseUrl(this.baseUrl)).toString();
  }

  private headers(): HeadersInit {
    if (this.apiKey.length === 0) {
      return envelopeHeaders();
    }
    return {
      ...envelopeHeaders(),
      Authorization: `Bearer ${this.apiKey}`
    };
  }

  private async parse<TData>(response: Response): Promise<EshuEnvelope<TData>> {
    if (!response.ok) {
      throw new Error(`Eshu API request failed with HTTP ${response.status}`);
    }
    const parsed = (await response.json()) as EshuEnvelope<TData>;
    return parsed;
  }

  private async parseJson<TData>(response: Response): Promise<TData> {
    if (!response.ok) {
      throw new Error(`Eshu API request failed with HTTP ${response.status}`);
    }
    const parsed = (await response.json()) as unknown;
    if (isEnvelope(parsed)) {
      return unwrapEnvelope(parsed as EshuEnvelope<TData>).data;
    }
    return parsed as TData;
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
