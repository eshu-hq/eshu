"""IP-free Flask service fixture: parsed route handlers (functions) bound to the
workload this repo materializes from its Dockerfile + k8s manifest (rc-2: RUNS_IN)."""
from flask import Flask

app = Flask(__name__)


@app.route("/api/orders")
def list_orders():
    return {"orders": []}


@app.route("/api/health")
def health():
    return {"status": "ok"}


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
