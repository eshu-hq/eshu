#!/usr/bin/env bash
# Manifest emitters for the live Kubernetes two-team hosted governance proof
# (#1910). scripts/run-k8s-two-team-governance-proof.sh sources this file and
# pipes each function's output to `kubectl apply -f -`. Splitting the large
# inline YAML out of the driver keeps the driver under the repo file-size cap and
# keeps the cluster shape reviewable in one place.
#
# These manifests carry no secret material themselves: the Postgres password is
# interpolated from the driver-generated ${pg_password}, and the seed image tag
# from ${seed_image}. Both are per-run, throwaway, and torn down by the driver.

# postgres_manifest emits a minimal single-replica Postgres (Secret + Deployment
# + Service) used as the chart's external content store for the proof. It uses an
# emptyDir volume because the namespace is created and deleted per run.
postgres_manifest() {
	# printf, not a heredoc: Homebrew bash >= 5.1 writes an entire heredoc
	# body to a pipe before forking the reader, and macOS's 512-byte pipe
	# buffer deadlocks on any body over that size (#5074). This body expands
	# "${pg_password}" on one line, so it cannot move to a static
	# scripts/lib/ data file; that line is double-quoted and every other
	# line is single-quoted to preserve the original heredoc's expansion
	# behavior byte for byte.
	printf '%s\n' \
		'apiVersion: v1' \
		'kind: Secret' \
		'metadata:' \
		'  name: postgres-auth' \
		'stringData:' \
		'  POSTGRES_USER: eshu' \
		"  POSTGRES_PASSWORD: ${pg_password}" \
		'  POSTGRES_DB: eshu' \
		'---' \
		'apiVersion: apps/v1' \
		'kind: Deployment' \
		'metadata:' \
		'  name: postgres' \
		'  labels:' \
		'    app.kubernetes.io/name: postgres' \
		'spec:' \
		'  replicas: 1' \
		'  selector:' \
		'    matchLabels:' \
		'      app.kubernetes.io/name: postgres' \
		'  template:' \
		'    metadata:' \
		'      labels:' \
		'        app.kubernetes.io/name: postgres' \
		'    spec:' \
		'      containers:' \
		'        - name: postgres' \
		'          image: postgres:18-alpine' \
		'          envFrom:' \
		'            - secretRef:' \
		'                name: postgres-auth' \
		'          env:' \
		'            - name: PGDATA' \
		'              value: /var/lib/postgresql/data/pgdata' \
		'          ports:' \
		'            - containerPort: 5432' \
		'          readinessProbe:' \
		'            exec:' \
		'              command: ["pg_isready", "-U", "eshu", "-d", "eshu"]' \
		'            initialDelaySeconds: 5' \
		'            periodSeconds: 5' \
		'          volumeMounts:' \
		'            - name: data' \
		'              mountPath: /var/lib/postgresql/data' \
		'      volumes:' \
		'        - name: data' \
		'          emptyDir: {}' \
		'---' \
		'apiVersion: v1' \
		'kind: Service' \
		'metadata:' \
		'  name: postgres' \
		'  labels:' \
		'    app.kubernetes.io/name: postgres' \
		'spec:' \
		'  selector:' \
		'    app.kubernetes.io/name: postgres' \
		'  ports:' \
		'    - port: 5432' \
		'      targetPort: 5432'
}

# seed_job_manifest emits the one-shot bootstrap-index Job that seeds two
# repositories from the filesystem fixtures baked into ${seed_image}. It writes
# facts to the same Postgres + NornicDB the chart workloads use.
seed_job_manifest() {
	# printf, not a heredoc: see postgres_manifest() above for the #5074
	# pipe-deadlock rationale. This body expands "${seed_image}" on one
	# line, so it cannot move to a static scripts/lib/ data file; that line
	# is double-quoted and every other line is single-quoted to preserve the
	# original heredoc's expansion behavior byte for byte.
	printf '%s\n' \
		'apiVersion: batch/v1' \
		'kind: Job' \
		'metadata:' \
		'  name: gov-seed' \
		'spec:' \
		'  backoffLimit: 1' \
		'  activeDeadlineSeconds: 600' \
		'  template:' \
		'    spec:' \
		'      restartPolicy: Never' \
		'      containers:' \
		'        - name: seed' \
		"          image: ${seed_image}" \
		'          imagePullPolicy: IfNotPresent' \
		'          command: ["/usr/local/bin/eshu-bootstrap-index"]' \
		'          env:' \
		'            - name: ESHU_GRAPH_BACKEND' \
		'              value: nornicdb' \
		'            - name: DEFAULT_DATABASE' \
		'              value: nornic' \
		'            - name: NEO4J_DATABASE' \
		'              value: nornic' \
		'            - name: NEO4J_URI' \
		'              value: bolt://eshu-nornicdb:7687' \
		'            - name: NEO4J_USERNAME' \
		'              value: neo4j' \
		'            - name: NEO4J_PASSWORD' \
		'              value: change-me' \
		'            - name: ESHU_HOME' \
		'              value: /tmp/.eshu' \
		'            - name: HOME' \
		'              value: /tmp' \
		'            - name: ESHU_REPOS_DIR' \
		'              value: /tmp/repos' \
		'            - name: ESHU_REPO_SOURCE_MODE' \
		'              value: filesystem' \
		'            - name: ESHU_FILESYSTEM_ROOT' \
		'              value: /fixtures' \
		'            - name: ESHU_GIT_AUTH_METHOD' \
		'              value: none' \
		'            - name: ESHU_DEPLOYMENT_ENVIRONMENT' \
		'              value: k8s-gov-proof' \
		'            - name: ESHU_CONTENT_STORE_DSN' \
		'              valueFrom:' \
		'                secretKeyRef:' \
		'                  name: eshu-content-store' \
		'                  key: dsn' \
		'            - name: ESHU_POSTGRES_DSN' \
		'              valueFrom:' \
		'                secretKeyRef:' \
		'                  name: eshu-content-store' \
		'                  key: dsn' \
		'          volumeMounts:' \
		'            - name: tmp' \
		'              mountPath: /tmp' \
		'      volumes:' \
		'        - name: tmp' \
		'          emptyDir: {}'
}
