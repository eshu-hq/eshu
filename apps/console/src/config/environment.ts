export type ConsoleMode = "demo" | "private";

export interface ConsoleEnvironment {
  readonly apiKey: string;
  readonly apiBaseUrl: string;
  readonly mode: ConsoleMode;
  readonly recentApiBaseUrls: readonly string[];
}

export const consoleStorageKeys = {
  environment: "eshu.console.environment"
} as const;

const defaultEnvironment: ConsoleEnvironment = {
  apiKey: import.meta.env.VITE_ESHU_API_KEY?.trim() ?? "",
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
    return normalizeEnvironment(parsed);
  } catch {
    return defaultEnvironment;
  }
}

export function saveConsoleEnvironment(environment: ConsoleEnvironment): void {
  const normalized = normalizeEnvironment(environment);
  window.localStorage.setItem(
    consoleStorageKeys.environment,
    JSON.stringify(normalized)
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
  const mode: ConsoleMode = environment.mode === "private" ? "private" : "demo";
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
