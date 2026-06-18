// Synthetic demo HTTP service. It imports the synthetic vulnerable dependency so
// the manifest -> lockfile -> import chain is present for the demo. No real
// network calls, no real registry, no proprietary code.
const http = require("http");
const { render } = require("synthetic-vulnerable-npm");

const port = Number(process.env.PORT || 8080);

const server = http.createServer((req, res) => {
  // render() is the synthetic vulnerable code path the advisory describes.
  res.writeHead(200, { "Content-Type": "text/plain" });
  res.end(render(req.url || "/"));
});

server.listen(port, () => {
  // Deterministic startup line for the demo logs.
  console.log(`synthetic-supply-chain-demo-app listening on :${port}`);
});
