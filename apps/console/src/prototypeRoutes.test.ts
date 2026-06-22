import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import { describe, expect, it } from "vitest";

interface RouteHelpers {
  readonly canonicalRoute: (route: string) => string;
  readonly publicRoute: (route: string) => string;
  readonly hashFor: (route: string, suffix?: string) => string;
}

function loadRoutes(): RouteHelpers {
  const win: { ESHU_ROUTES?: RouteHelpers } = {};
  const path = resolve(dirname(fileURLToPath(import.meta.url)), "../prototype/eshu-console/console/routes.js");
  vm.runInNewContext(readFileSync(path, "utf8"), { window: win, Object });
  if (win.ESHU_ROUTES === undefined) throw new Error("route helpers did not load");
  return win.ESHU_ROUTES;
}

describe("prototype route helpers", () => {
  it("accept old demo hashes while emitting live console route hashes", () => {
    const routes = loadRoutes();

    expect(routes.canonicalRoute("repos")).toBe("repos");
    expect(routes.canonicalRoute("repositories")).toBe("repos");
    expect(routes.canonicalRoute("dead-code")).toBe("deadcode");
    expect(routes.canonicalRoute("code-graph")).toBe("codegraph");
    expect(routes.canonicalRoute("operations")).toBe("admin");
    expect(routes.canonicalRoute("workspace/repositories/repository:r_1")).toBe("workspace");
    expect(routes.canonicalRoute("workspace/services/svc-platform")).toBe("workspace");
    expect(routes.canonicalRoute("repositories/repository:r_1/source")).toBe("reposource");

    expect(routes.publicRoute("repos")).toBe("repositories");
    expect(routes.publicRoute("deadcode")).toBe("dead-code");
    expect(routes.publicRoute("codegraph")).toBe("code-graph");
    expect(routes.publicRoute("admin")).toBe("operations");
    expect(routes.hashFor("vulnerabilities", "?cve=CVE-2026-0001")).toBe("#vulnerabilities?cve=CVE-2026-0001");
    expect(routes.hashFor("workspace", "/services/svc-platform")).toBe("#workspace/services/svc-platform");
    expect(routes.hashFor("reposource", "/repository:r_1/source?path=server%2Findex.ts")).toBe("#repositories/repository:r_1/source?path=server%2Findex.ts");
  });
});
