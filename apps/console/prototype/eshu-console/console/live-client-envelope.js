/* Normalizes standalone prototype API client envelope errors. */
(function () {
  "use strict";
  if (!window.ESHU || !window.ESHU.EshuApiClient) return;

  function envelopeErrorMessage(error) {
    if (!error) return "";
    if (typeof error === "string") return error;
    if (typeof error !== "object") return "api error";
    return String(error.message || error.code || "api error");
  }

  function rejectErrorEnvelope(env) {
    const message = envelopeErrorMessage(env && env.error);
    if (message) throw new Error(message);
    return env;
  }

  const proto = window.ESHU.EshuApiClient.prototype;
  const baseGet = proto.get;
  const basePost = proto.post;

  proto.get = async function get(path) {
    return rejectErrorEnvelope(await baseGet.call(this, path));
  };

  proto.post = async function post(path, body) {
    return rejectErrorEnvelope(await basePost.call(this, path, body));
  };
})();
