export type ConsoleMode = "demo" | "private";

export interface ConsoleEnvironment {
  readonly apiKey: string;
  readonly apiBaseUrl: string;
  readonly mode: ConsoleMode;
  readonly recentApiBaseUrls: readonly string[];
}

// PersistedEnvironment is the subset written to web storage. It intentionally
// omits apiKey: a bearer token in localStorage is exfiltrable via XSS, so the
// key is kept in memory for the session only and never serialized.
type PersistedEnvironment = Omit<ConsoleEnvironment, "apiKey">;

export const consoleStorageKeys = {
  environment: "eshu.console.environment"
} as const;

// Build-time key injection (e.g. local dev via VITE_ESHU_API_KEY) seeds the
// in-memory key. It is never written back to web storage. Vite types
// `import.meta.env` as `Record<string, any>`, so we narrow the value through
// a tiny reader that treats anything non-string as empty.
const rawApiKeyEnv: unknown = import.meta.env.VITE_ESHU_API_KEY;
const defaultApiKey: string = typeof rawApiKeyEnv === "string" ? rawApiKeyEnv.trim() : "";

const defaultEnvironment: ConsoleEnvironment = {
  apiKey: defaultApiKey,
  apiBaseUrl: "/eshu-api/",
  mode: "private",
  recentApiBaseUrls: []
};

export function loadConsoleEnvironment(): ConsoleEnvironment {
  const rawValue = window.localStorage.getItem(consoleStorageKeys.environment);
  if (rawValue === null) {
    return defaultEnvironment;
  }

  try {
    const parsed = JSON.parse(rawValue) as Partial<ConsoleEnvironment>;
    // Discard any apiKey from a previously persisted payload; the key is sourced
    // only from the in-memory build-time default.
    return normalizeEnvironment({ ...parsed, apiKey: defaultApiKey });
  } catch {
    return defaultEnvironment;
  }
}

export function saveConsoleEnvironment(environment: ConsoleEnvironment): void {
  const normalized = normalizeEnvironment(environment);
  const persisted: PersistedEnvironment = {
    apiBaseUrl: normalized.apiBaseUrl,
    mode: normalized.mode,
    recentApiBaseUrls: normalized.recentApiBaseUrls
  };
  window.localStorage.setItem(
    consoleStorageKeys.environment,
    JSON.stringify(persisted)
  );
}

function normalizeEnvironment(
  environment: Partial<ConsoleEnvironment>
): ConsoleEnvironment {
  const apiBaseUrl = typeof environment.apiBaseUrl === "string"
    ? environment.apiBaseUrl.trim()
    : "";
  const apiKey = typeof environment.apiKey === "string"
    ? environment.apiKey.trim()
    : "";
  const mode: ConsoleMode = environment.mode === "demo" ? "demo" : "private";
  const savedRecent = Array.isArray(environment.recentApiBaseUrls)
    ? environment.recentApiBaseUrls.filter(isNonEmptyString)
    : [];
  const recentApiBaseUrls = [...new Set([apiBaseUrl, ...savedRecent].filter(isNonEmptyString))]
    .slice(0, 5);

  return {
    apiKey,
    apiBaseUrl,
    mode,
    recentApiBaseUrls
  };
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}
