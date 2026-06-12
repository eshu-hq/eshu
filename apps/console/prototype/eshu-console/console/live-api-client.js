/* Envelope-preserving live API client for the standalone prototype. */
(function () {
  "use strict";
  if (!window.ESHU) return;

  function EshuApiClient(opts) {
    opts = opts || {};
    const base = (opts.baseUrl || "/eshu-api/").trim();
    this.baseUrl = base.endsWith("/") ? base : base + "/";
    this.apiKey = (opts.apiKey || "").trim();
  }

  EshuApiClient.prototype._url = function (path) {
    const cleanPath = path.startsWith("/") ? path.slice(1) : path;
    const origin = (typeof location !== "undefined" && location.origin) || "http://localhost";
    const base = /^https?:\/\//.test(this.baseUrl) ? this.baseUrl : new URL(this.baseUrl, origin).toString();
    return new URL(cleanPath, base).toString();
  };

  EshuApiClient.prototype._headers = function (extra) {
    const headers = Object.assign({ Accept: "application/eshu.envelope+json" }, extra || {});
    if (this.apiKey) headers.Authorization = "Bearer " + this.apiKey;
    return headers;
  };

  EshuApiClient.prototype.get = async function (path) {
    const response = await fetch(this._url(path), { headers: this._headers() });
    if (!response.ok) throw new Error("HTTP " + response.status);
    return await response.json();
  };

  EshuApiClient.prototype.post = async function (path, body) {
    const response = await fetch(this._url(path), {
      method: "POST",
      headers: this._headers({ "Content-Type": "application/json" }),
      body: JSON.stringify(body || {})
    });
    if (!response.ok) throw new Error("HTTP " + response.status);
    return await response.json();
  };

  window.ESHU.EshuApiClient = EshuApiClient;
})();
