/* Route parity helpers for the standalone prototype.
   The prototype keeps its historic internal route ids, but public hashes should
   match the live React console so design-tool flows do not drift. */
(function () {
  const ROUTE_ALIASES = {
    repositories: "repos",
    "dead-code": "deadcode",
    "code-graph": "codegraph",
    operations: "admin"
  };
  const PUBLIC_ROUTES = {
    repos: "repositories",
    deadcode: "dead-code",
    codegraph: "code-graph",
    admin: "operations"
  };

  function canonicalRoute(route) {
    if (route.indexOf("workspace/") === 0) return "workspace";
    if (route.indexOf("repositories/") === 0 && route.indexOf("/source") > 0) return "reposource";
    return ROUTE_ALIASES[route] || route;
  }

  function publicRoute(route) {
    const canonical = canonicalRoute(route);
    if (canonical === "reposource") return "repositories";
    return PUBLIC_ROUTES[canonical] || canonical;
  }

  function hashFor(route, suffix) {
    if (canonicalRoute(route) === "workspace" && suffix && suffix.indexOf("/") === 0) {
      return "#workspace" + suffix;
    }
    if (canonicalRoute(route) === "reposource" && suffix && suffix.indexOf("/") === 0) {
      return "#repositories" + suffix;
    }
    return "#" + publicRoute(route) + (suffix || "");
  }

  function setHash(route, suffix) {
    if (canonicalRoute(route) === "workspace" && suffix && suffix.indexOf("/") === 0) {
      window.location.hash = "workspace" + suffix;
      return;
    }
    if (canonicalRoute(route) === "reposource" && suffix && suffix.indexOf("/") === 0) {
      window.location.hash = "repositories" + suffix;
      return;
    }
    window.location.hash = publicRoute(route) + (suffix || "");
  }

  window.ESHU_ROUTES = Object.freeze({
    canonicalRoute,
    publicRoute,
    hashFor,
    setHash
  });
})();
