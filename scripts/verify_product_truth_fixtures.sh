#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="$REPO_ROOT/tests/fixtures/product_truth/manifest.json"

require_file() {
    local path="$1"
    [[ -f "$path" ]] || {
        echo "Missing required file: $path" >&2
        return 1
    }
}

require_dir() {
    local path="$1"
    [[ -d "$path" ]] || {
        echo "Missing required directory: $path" >&2
        return 1
    }
    [[ ! -L "$path" ]] || {
        echo "Fixture directory must not be a symlink: $path" >&2
        return 1
    }
}

require_executable() {
    local path="$1"
    [[ -x "$path" ]] || {
        echo "Expected executable verifier: $path" >&2
        return 1
    }
}

require_json_query() {
    local file="$1"
    local query="$2"
    local description="$3"
    jq -e "$query" "$file" >/dev/null || {
        echo "$description" >&2
        echo "File: $file" >&2
        return 1
    }
}

require_file "$MANIFEST"
require_json_query "$MANIFEST" '.schema_version == 1' "Product truth manifest schema_version must be 1"
require_json_query "$MANIFEST" '(.suites // []) | length >= 4' "Product truth manifest must register owned fixture suites"
require_json_query "$MANIFEST" '(.suites // []) | any(.id == "iac_quality.dead_iac" and .status == "owned")' \
    "Product truth manifest must track dead-IaC as an owned capability"

while IFS= read -r suite; do
    id="$(jq -r '.id' <<<"$suite")"
    status="$(jq -r '.status' <<<"$suite")"
    fixture_root="$REPO_ROOT/$(jq -r '.fixture_root' <<<"$suite")"
    compose_gate="$REPO_ROOT/$(jq -r '.compose_gate' <<<"$suite")"

    if [[ "$status" != "owned" ]]; then
        echo "Skipping non-owned suite $id ($status)"
        continue
    fi

    require_dir "$fixture_root"
    require_executable "$compose_gate"
    require_json_query <(printf '%s' "$suite") '(.capabilities // []) | length > 0' \
        "Suite $id must list capabilities"
    require_json_query <(printf '%s' "$suite") '(.expected_truth_files // []) | length > 0' \
        "Suite $id must list expected truth files"

    while IFS= read -r expected_file; do
        expected_path="$REPO_ROOT/$expected_file"
        require_file "$expected_path"
        case "$expected_path" in
            *.json)
                require_json_query "$expected_path" '.schema_version == 1' \
                    "Expected truth file must declare schema_version=1: $expected_file"
                require_json_query "$expected_path" '(.capability_assertions // []) | length > 0' \
                    "Expected truth file must include capability assertions: $expected_file"
                ;;
            *.yaml|*.yml)
                # Existing fixture contracts may be YAML. Their runtime verifier
                # owns semantic validation; this static gate only checks presence.
                ;;
            *)
                echo "Unsupported expected truth file type: $expected_file" >&2
                exit 1
                ;;
        esac
    done < <(jq -r '.expected_truth_files[]' <<<"$suite")

    echo "verified product truth suite: $id"
done < <(jq -c '.suites[]' "$MANIFEST")

DEAD_IAC_ROOT="$REPO_ROOT/tests/fixtures/product_truth/dead_iac"
DEAD_IAC_EXPECTED="$REPO_ROOT/tests/fixtures/product_truth/expected/dead_iac.json"
require_dir "$DEAD_IAC_ROOT"
require_file "$DEAD_IAC_EXPECTED"
require_json_query "$DEAD_IAC_EXPECTED" \
    '.schema_version == 1 and .suite_id == "iac_quality.dead_iac" and ((.capability_assertions // []) | length) >= 9' \
    "Dead-IaC expected truth must define schema_version=1 and at least nine assertions"
require_json_query "$DEAD_IAC_EXPECTED" \
    '.capability_assertions as $assertions | all(["terraform","helm","kustomize","ansible","compose"][]; . as $family | any($assertions[]; .family == $family and .expected_reachability == "used") and any($assertions[]; .family == $family and .expected_reachability == "unused") and any($assertions[]; .family == $family and .expected_reachability == "ambiguous"))' \
    "Dead-IaC expected truth must cover used, unused, and ambiguous cases for Terraform, Helm, Kustomize, Ansible, and Compose"

require_file "$DEAD_IAC_ROOT/terraform-stack/main.tf"
require_file "$DEAD_IAC_ROOT/terraform-stack/terragrunt.hcl"
require_file "$DEAD_IAC_ROOT/terraform-modules/modules/checkout-service/main.tf"
require_file "$DEAD_IAC_ROOT/terraform-modules/modules/orphan-cache/main.tf"
require_file "$DEAD_IAC_ROOT/terraform-modules/modules/dynamic-target/main.tf"
require_file "$DEAD_IAC_ROOT/helm-controller/argocd/applications/checkout-service.yaml"
require_file "$DEAD_IAC_ROOT/helm-controller/argocd/applications/dynamic-target.yaml"
require_file "$DEAD_IAC_ROOT/helm-controller/.github/workflows/deploy-worker.yaml"
require_file "$DEAD_IAC_ROOT/helm-charts/charts/checkout-service/Chart.yaml"
require_file "$DEAD_IAC_ROOT/helm-charts/charts/worker-service/Chart.yaml"
require_file "$DEAD_IAC_ROOT/helm-charts/charts/orphan-worker/Chart.yaml"
require_file "$DEAD_IAC_ROOT/helm-charts/charts/dynamic-target/Chart.yaml"
require_file "$DEAD_IAC_ROOT/ansible-controller/.github/workflows/deploy-ops.yaml"
require_file "$DEAD_IAC_ROOT/ansible-controller/jenkins/Jenkinsfile"
require_file "$DEAD_IAC_ROOT/ansible-ops/playbooks/site.yml"
require_file "$DEAD_IAC_ROOT/ansible-ops/playbooks/dynamic-role.yml"
require_file "$DEAD_IAC_ROOT/ansible-ops/playbooks/orphan-maintenance.yml"
require_file "$DEAD_IAC_ROOT/ansible-ops/roles/checkout_deploy/tasks/main.yml"
require_file "$DEAD_IAC_ROOT/ansible-ops/roles/orphan_maintenance/tasks/main.yml"
require_file "$DEAD_IAC_ROOT/ansible-ops/roles/dynamic_role/tasks/main.yml"
require_file "$DEAD_IAC_ROOT/kustomize-controller/argocd/applications/checkout-prod.yaml"
require_file "$DEAD_IAC_ROOT/kustomize-controller/argocd/applications/dynamic-target.yaml"
require_file "$DEAD_IAC_ROOT/kustomize-config/overlays/prod/kustomization.yaml"
require_file "$DEAD_IAC_ROOT/kustomize-config/base/checkout-service/kustomization.yaml"
require_file "$DEAD_IAC_ROOT/kustomize-config/base/orphan-api/kustomization.yaml"
require_file "$DEAD_IAC_ROOT/kustomize-config/base/dynamic-target/kustomization.yaml"
require_file "$DEAD_IAC_ROOT/compose-controller/.github/workflows/deploy-compose.yaml"
require_file "$DEAD_IAC_ROOT/compose-app/compose.yaml"

echo "Product truth fixture contract verified."
