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
      apiBaseUrl: "/eshu-api/",
      mode: "private",
      recentApiBaseUrls: []
    });
  });

  it("persists private API endpoints and recent environments", () => {
    saveConsoleEnvironment({
      apiBaseUrl: "http://localhost:8080",
      mode: "private",
      recentApiBaseUrls: ["https://eshu.internal"]
    });

    expect(window.localStorage.getItem(consoleStorageKeys.environment)).toContain(
      "localhost"
    );
    expect(loadConsoleEnvironment()).toEqual({
      apiBaseUrl: "http://localhost:8080",
      mode: "private",
      recentApiBaseUrls: ["http://localhost:8080", "https://eshu.internal"]
    });
  });
});
