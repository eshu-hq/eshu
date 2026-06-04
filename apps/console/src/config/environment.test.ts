import {
  consoleStorageKeys,
  loadConsoleEnvironment,
  saveConsoleEnvironment
} from "./environment";

describe("console environment config", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("defaults to the local Eshu API proxy", () => {
    expect(loadConsoleEnvironment()).toEqual({
      apiKey: "",
      apiBaseUrl: "/eshu-api/",
      mode: "private",
      recentApiBaseUrls: []
    });
  });

  it("persists private API endpoints and recent environments", () => {
    saveConsoleEnvironment({
      apiKey: " local-compose-token ",
      apiBaseUrl: "http://localhost:8080",
      mode: "private",
      recentApiBaseUrls: ["https://eshu.internal"]
    });

    expect(window.localStorage.getItem(consoleStorageKeys.environment)).toContain(
      "localhost"
    );
    // The bearer token is never written to web storage; it is held in memory for
    // the session only, so a reload starts with an empty key.
    expect(loadConsoleEnvironment()).toEqual({
      apiKey: "",
      apiBaseUrl: "http://localhost:8080",
      mode: "private",
      recentApiBaseUrls: ["http://localhost:8080", "https://eshu.internal"]
    });
  });

  it("never serializes the API key to web storage", () => {
    saveConsoleEnvironment({
      apiKey: "local-compose-token",
      apiBaseUrl: "http://localhost:8080",
      mode: "private",
      recentApiBaseUrls: []
    });

    const raw = window.localStorage.getItem(consoleStorageKeys.environment) ?? "";
    expect(raw).not.toContain("local-compose-token");
    expect(JSON.parse(raw)).not.toHaveProperty("apiKey");
  });

  it("ignores an apiKey from a legacy persisted payload", () => {
    window.localStorage.setItem(
      consoleStorageKeys.environment,
      JSON.stringify({
        apiKey: "legacy-token",
        apiBaseUrl: "http://localhost:8080",
        mode: "private",
        recentApiBaseUrls: []
      })
    );

    expect(loadConsoleEnvironment().apiKey).toBe("");
  });
});
