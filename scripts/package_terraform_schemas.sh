#!/usr/bin/env bash
#
# Package Terraform provider schemas for distribution.
#
# Reads raw JSON schemas from schemas/, extracts the resolved provider version
# from the corresponding .terraform.lock.hcl, and produces versioned gzipped
# files in the Go-owned packaged schema directory.
#
# Naming convention: <provider>-<version>.json.gz
#   e.g. aws-5.100.0.json.gz, google-6.50.0.json.gz
#
# Usage:
#   ./scripts/package_terraform_schemas.sh          # all providers
#   ./scripts/package_terraform_schemas.sh aws      # specific provider
#
# Prerequisites:
#   - Raw schemas already generated via generate_terraform_provider_schema.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCHEMAS_DIR="$REPO_ROOT/schemas"
PROVIDERS_DIR="$REPO_ROOT/terraform_providers"
PACKAGE_DIR="$REPO_ROOT/go/internal/terraformschema/schemas"

mkdir -p "$PACKAGE_DIR"

package_schema() {
    local provider="$1"
    local raw_schema="$SCHEMAS_DIR/$provider.json"
    local lock_file="$PROVIDERS_DIR/$provider/.terraform.lock.hcl"

    if [[ ! -f "$raw_schema" ]]; then
        echo "ERROR: No raw schema at $raw_schema — run generate_terraform_provider_schema.sh first" >&2
        return 1
    fi

    # Extract version from lock file.
    local version
    if [[ -f "$lock_file" ]]; then
        version=$(rg -U -o 'version\\s*=\\s*"[^\"]+"' "$lock_file" | head -1 | sed 's/.*"//' | sed 's/"$//')
    else
        echo "WARNING: No lock file at $lock_file — using 'unknown' version" >&2
        version="unknown"
    fi

    local output_file="$PACKAGE_DIR/$provider-$version.json.gz"

    # Remove any existing schemas for this provider (old versions).
    rm -f "$PACKAGE_DIR/$provider"-*.json.gz

    echo "==> Packaging $provider v$version"
    gzip -c "$raw_schema" > "$output_file"

    local compressed_size
    compressed_size=$(du -h "$output_file" | cut -f1)
    echo "    Output: $output_file ($compressed_size)"
}

# Determine which providers to package.
if [[ $# -gt 0 ]]; then
    providers=("$@")
else
    providers=()
    for schema_file in "$SCHEMAS_DIR"/*.json; do
        [[ -f "$schema_file" ]] && providers+=("$(basename "$schema_file" .json)")
    done
fi

if [[ ${#providers[@]} -eq 0 ]]; then
    echo "No schema files found in $SCHEMAS_DIR/" >&2
    exit 1
fi

for provider in "${providers[@]}"; do
    package_schema "$provider"
done

echo ""
echo "Packaged schemas in $PACKAGE_DIR/"
ls -lh "$PACKAGE_DIR/"*.json.gz
echo ""
echo "These files are committed to git and ship with the Go runtime."
echo "Run tests: (cd go && go test ./internal/terraformschema ./internal/relationships -count=1)"
