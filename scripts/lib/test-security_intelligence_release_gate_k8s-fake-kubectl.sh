#!/usr/bin/env bash
set -euo pipefail
args="$*"
case "${args}" in
  "-n eshu get pods -o json")
    # printf, not a heredoc: this 1046-byte body sits in the 512B-64KB
    # Homebrew-bash-5.1+ pipe-deadlock zone (#5074). It has no variable
    # expansion, so every line is single-quoted.
    printf '%s\n' \
    '{"items":[' \
    '  {"metadata":{"name":"eshu-api-6bd9f-private","labels":{"app.kubernetes.io/name":"eshu","app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"api"}},"spec":{"nodeName":"node.private.internal","containers":[{"name":"api","image":"registry.private.example.com/eshu/api:dev","resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"cpu":"1","memory":"1Gi"}}}]},"status":{"phase":"Running","podIP":"10.42.0.15","hostIP":"172.20.1.11","containerStatuses":[{"name":"api","ready":true,"restartCount":1}]}},' \
    '  {"metadata":{"name":"eshu-reducer-77dd-private","labels":{"app.kubernetes.io/name":"eshu","app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"reducer"}},"spec":{"nodeName":"node.private.internal","containers":[{"name":"reducer","resources":{"requests":{"cpu":"500m","memory":"512Mi"},"limits":{"cpu":"2","memory":"2Gi"}}}]},"status":{"phase":"Running","podIP":"10.42.0.16","hostIP":"172.20.1.11","containerStatuses":[{"name":"reducer","ready":true,"restartCount":0}]}}' \
    ']}'
    ;;
  "-n eshu top pods --no-headers")
    printf 'eshu-api-6bd9f-private 12m 190Mi\n'
    printf 'eshu-reducer-77dd-private 80m 512Mi\n'
    ;;
  "-n eshu get servicemonitors -o json")
    if [ "${ESHU_FAKE_K8S_NO_SERVICE_MONITOR:-0}" = "1" ]; then
      printf '{"items":[]}\n'
      exit 0
    fi
    # printf, not a heredoc: this 557-byte body is in the same #5074
    # deadlock zone as the pods JSON above; no expansion, single-quoted.
    printf '%s\n' \
    '{"items":[' \
    '  {"metadata":{"name":"private-release-api","labels":{"app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"api"}},"spec":{"endpoints":[{"path":"/metrics","port":"metrics"}],"selector":{"matchLabels":{"app.kubernetes.io/component":"api"}}}},' \
    '  {"metadata":{"name":"private-release-reducer","labels":{"app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"reducer"}},"spec":{"endpoints":[{"path":"/metrics","port":"metrics"}],"selector":{"matchLabels":{"app.kubernetes.io/component":"reducer"}}}}' \
    ']}'
    ;;
  "-n eshu get networkpolicies -o json")
    cat <<'JSON'
{"items":[
  {"metadata":{"name":"private-release-network","labels":{"app.kubernetes.io/instance":"private-release"}},"spec":{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"eshu"}},"policyTypes":["Ingress","Egress"]}}
]}
JSON
    ;;
  "-n eshu get poddisruptionbudgets -o json")
    cat <<'JSON'
{"items":[
  {"metadata":{"name":"private-release-api","labels":{"app.kubernetes.io/component":"api"}},"status":{"currentHealthy":1,"desiredHealthy":1,"disruptionsAllowed":0}}
]}
JSON
    ;;
  "-n eshu get jobs -o json")
    if [ "${ESHU_FAKE_K8S_BAD_BOOTSTRAP:-0}" = "1" ]; then
      cat <<'JSON'
{"items":[
  {"metadata":{"name":"private-release-schema-bootstrap","labels":{"app.kubernetes.io/component":"schema-bootstrap"}},"status":{"active":0,"succeeded":0,"failed":1,"conditions":[{"type":"Failed","status":"True"}]}}
]}
JSON
      exit 0
    fi
    cat <<'JSON'
{"items":[
  {"metadata":{"name":"private-release-schema-bootstrap","labels":{"app.kubernetes.io/component":"schema-bootstrap"}},"status":{"active":0,"succeeded":1,"failed":0,"conditions":[{"type":"Complete","status":"True"}]}}
]}
JSON
    ;;
  "-n eshu logs eshu-api-6bd9f-private --all-containers --tail=200")
    printf 'level=info repository=private/repo package_name=private-package provider_url=https://provider.private.example.com/path token=ghp_abcdef path=/Users/alice/private/repo host=api.private.example.com ip=10.42.0.15 PASSWORD: hunter2 AWS_SECRET_ACCESS_KEY: aws-secret apiKey: api-key-secret client_secret: client-secret Authorization: Bearer bearer-secret role_arn=arn:aws:iam::123456789012:role/private-role\n'
    ;;
  "-n eshu logs eshu-reducer-77dd-private --all-containers --tail=200")
    printf '{"level":"warn","repo":"private/repo","package":"private-package","url":"https://provider.private.example.com/path","message":"retrying queue item","host":"reducer.private.example.com","path":"/home/alice/private/repo","PASSWORD":"hunter2","AWS_SECRET_ACCESS_KEY":"aws-secret","apiKey":"api-key-secret","client_secret":"client-secret","authorization":"Bearer bearer-secret","role_arn":"arn:aws:iam::123456789012:role/private-role"}\n'
    ;;
  *)
    printf 'unexpected kubectl args: %s\n' "${args}" >&2
    exit 1
    ;;
esac
