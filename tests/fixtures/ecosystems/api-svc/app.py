"""Orders API service (rc-2 fixture).

A Flask app whose route handlers are deployed as a Kubernetes workload, so the
golden-corpus gate can assert the code->runtime bridge edge
(Function)-[:RUNS_IN]->(Workload): the parser detects the @app.route handlers,
HANDLES_ROUTE binds each handler Function to its Endpoint, and runs_in binds the
handler Function to the Workload this repository DEFINES via k8s/deployment.yaml.
"""
from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/api/orders")
def list_orders():
    """Return the orders collection."""
    return jsonify([])


@app.route("/api/health")
def health():
    """Liveness probe."""
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8000)
