#!/usr/bin/env bash
set -euo pipefail
ENVIRONMENT="${ENVIRONMENT:-jenkins}"
ansible-playbook deploy.yml -i inventory/dynamic_hosts.py --limit "${TARGET_ENV:-prod}"
