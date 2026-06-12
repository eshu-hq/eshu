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
    return ROUTE_ALIASES[route] || route;
  }

  function publicRoute(route) {
    const canonical = canonicalRoute(route);
    return PUBLIC_ROUTES[canonical] || canonical;
  }

  function hashFor(route, suffix) {
    return "#" + publicRoute(route) + (suffix || "");
  }

  function setHash(route, suffix) {
    window.location.hash = publicRoute(route) + (suffix || "");
  }

  window.ESHU_ROUTES = Object.freeze({
    canonicalRoute,
    publicRoute,
    hashFor,
    setHash
  });
})();
