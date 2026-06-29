#!/usr/bin/env bash
set -euo pipefail

INPUT_FILE=""
OUTPUT_JSON=""
OUTPUT_MARKDOWN=""

usage() {
	cat <<'USAGE' >&2
Usage: verify_remote_e2e_degradation_report.sh --input evidence.json --output-json report.json --output-markdown report.md

Builds a public-safe full-corpus degradation report from an offline evidence
bundle. The report separates startup, graph-write, search-tail, schema-lock,
finite-completion, and hosted-collector health so follow-up issues can be
routed without publishing raw machine state.
USAGE
}

while (($# > 0)); do
	case "$1" in
		--input)
			INPUT_FILE="${2:-}"
			shift 2
			;;
		--output-json)
			OUTPUT_JSON="${2:-}"
			shift 2
			;;
		--output-markdown)
			OUTPUT_MARKDOWN="${2:-}"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "unexpected argument: $1" >&2
			usage
			exit 2
			;;
	esac
done

if [[ -z "${INPUT_FILE}" || -z "${OUTPUT_JSON}" || -z "${OUTPUT_MARKDOWN}" ]]; then
	usage
	exit 2
fi
if [[ ! -f "${INPUT_FILE}" ]]; then
	echo "input evidence bundle does not exist: ${INPUT_FILE}" >&2
	exit 1
fi

jq -e . "${INPUT_FILE}" >/dev/null

unsafe_match_file="$(mktemp)"
trap 'rm -f "${unsafe_match_file}"' EXIT
unsafe_pattern='(/Users/|/home/|/var/folders/|/private/|BEGIN [A-Z ]*PRIVATE KEY|gh[pousr]_[A-Za-z0-9_]+|AKIA[0-9A-Z]{16}|(^|[^0-9])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9]|$)|(^|[^0-9])[0-9]{12}([^0-9]|$))'
if jq -r '
	def sensitive_path:
		[.[]
			| tostring
			| select(test("account|tenant|customer|secret|token|password|credential|key"; "i"))
		] | length > 0;

	paths(scalars) as $path
	| getpath($path) as $value
	| if ($value | type) == "string" then
		$value
	elif (($value | type) == "number")
		and (($value | tostring) | test("^[0-9]{12}$"))
		and ($path | sensitive_path) then
		($value | tostring)
	else
		empty
	end
' "${INPUT_FILE}" | rg -n "${unsafe_pattern}" >"${unsafe_match_file}"; then
	echo "input evidence bundle is not public-safe; redact host paths, IPs, credentials, and account identifiers first" >&2
	sed -n '1,20p' "${unsafe_match_file}" >&2
	exit 1
fi

mkdir -p "$(dirname "${OUTPUT_JSON}")" "$(dirname "${OUTPUT_MARKDOWN}")"

jq '
	def evidence_text:
		.work.retrying_by_failure_class[]?.failure_class,
		.work.retrying_by_failure_class[]?.message,
		.work.pending_domains[]?.domain,
		.work.pending_domains[]?.reason,
		.postgres.active_queries[]?.query_shape,
		.logs[]?.message;

	def count_queue($name):
		(.index_status.queue[$name] // 0);

	def total_retrying:
		([
			.work.retrying_by_failure_class[]?.count,
			count_queue("retrying")
		] | add // 0);

	def evidence_has_text($pattern):
		[evidence_text | tostring | ascii_downcase | select(test($pattern))] | length > 0;

	def startup_failed:
		[.startup[]? | tostring | ascii_downcase | select(. != "passed" and . != "ok" and . != "healthy")] | length > 0;

	def all_startup_passed:
		((.startup // {}) | length) > 0 and (startup_failed | not);

	def unhealthy_services:
		[
			.services[]?
			| select(((.state // "") != "running") or (((.health // "healthy") != "healthy") and ((.health // "none") != "none")))
			| .name
		];

	def graph_timeout_observed:
		evidence_has_text("graph.*timeout|canonical.*retract.*timeout|canonical source-local retract|graph_canonical_retract_timeout");

	def search_tail_observed:
		evidence_has_text("active_docs|eshu_search_index_terms|search_document_readiness|search index|search-index");

	def schema_lock_observed:
		((.postgres.ungranted_locks // 0) > 0) or evidence_has_text("schema.*lock|ddl.*lock|database is locked|lock wait");

	def finite_degraded:
		((.index_status.status // "unknown") as $status
			| ($status != "healthy" and $status != "complete" and $status != "passed"))
		or ((count_queue("outstanding") + count_queue("pending") + count_queue("in_flight") + total_retrying + count_queue("failed") + count_queue("dead_letter")) > 0);

	def class($status; $evidence):
		{"status": $status, "evidence": $evidence};

	def startup_class:
		if all_startup_passed then
			class("passed"; ["startup/schema/bootstrap reported passed"])
		elif startup_failed then
			class("failed"; ["startup/schema/bootstrap reported a failure"])
		else
			class("unknown"; ["startup evidence was not supplied"])
		end;

	def hosted_collectors_class:
		if ((.services // []) | length) == 0 then
			class("unknown"; ["service health evidence was not supplied"])
		elif ((unhealthy_services | length) == 0) then
			class("passed"; ["all supplied hosted services are running and healthy"])
		else
			class("degraded"; [("unhealthy services: " + (unhealthy_services | join(", ")))])
		end;

	def graph_class:
		if graph_timeout_observed then
			class("blocked"; ["canonical graph write/retract timeout evidence observed"])
		else
			class("not_observed"; ["no graph write timeout evidence in supplied bundle"])
		end;

	def search_class:
		if search_tail_observed then
			class("blocked"; ["search/content maintenance tail evidence observed"])
		else
			class("not_observed"; ["no search maintenance tail evidence in supplied bundle"])
		end;

	def schema_lock_class:
		if schema_lock_observed then
			class("blocked"; ["schema DDL lock wait evidence observed"])
		else
			class("not_observed"; ["no ungranted schema lock evidence in supplied bundle"])
		end;

	def finite_completion_class:
		if finite_degraded then
			class("degraded"; [("index status=" + (.index_status.status // "unknown")), ("queue outstanding=" + ((count_queue("outstanding")) | tostring))])
		else
			class("passed"; ["finite completion queue is terminal"])
		end;

	def report_status($classes):
		if (($classes.graph_write_timeout.status == "blocked")
			or ($classes.search_index_tail.status == "blocked")
			or ($classes.schema_lock_wait.status == "blocked")
			or ($classes.finite_completion.status == "degraded")
			or ($classes.hosted_collectors.status == "degraded")
			or ($classes.startup.status == "failed")) then
			"degraded"
		elif (($classes.startup.status == "unknown") or ($classes.hosted_collectors.status == "unknown")) then
			"incomplete"
		else
			"passed"
		end;

	def pending_domain_summary:
		[
			.work.pending_domains[]?
			| {
				domain: (.domain // "unknown"),
				pending: (.pending // 0),
				oldest_age_seconds: (.oldest_age_seconds // 0)
			}
		];

	def retrying_failure_summary:
		[
			.work.retrying_by_failure_class[]?
			| {
				failure_class: (.failure_class // "unknown"),
				count: (.count // 0)
			}
		];

	def relation_size_summary:
		[
			.postgres.relation_sizes[]?
			| {
				name: (.name // "unknown"),
				bytes: (.bytes // 0)
			}
		];

	def service_health_summary:
		[
			.services[]?
			| {
				name: (.name // "unknown"),
				state: (.state // "unknown"),
				health: (.health // "unknown")
			}
		];

	def active_query_summary:
		[
			.postgres.active_queries[]?
			| {
				age_seconds: (.age_seconds // 0),
				query_shape: (.query_shape // "unknown")
			}
		] | sort_by(.age_seconds) | reverse | .[0:5];

	{
		startup: startup_class,
		graph_write_timeout: graph_class,
		search_index_tail: search_class,
		schema_lock_wait: schema_lock_class,
		finite_completion: finite_completion_class,
		hosted_collectors: hosted_collectors_class
	} as $classes
	| {
		schema_version: 1,
		run: {
			id: (.run.id // "unknown"),
			commit: (.run.commit // "unknown"),
			nornicdb_image: (.run.nornicdb_image // "unknown"),
			nornicdb_digest: (.run.nornicdb_digest // "unknown")
		},
		summary: {
			status: report_status($classes),
			queue: (.index_status.queue // {}),
			top_pending_domains: pending_domain_summary,
			retrying_by_failure_class: retrying_failure_summary,
			relation_sizes: relation_size_summary,
			service_health: service_health_summary,
			oldest_active_queries: active_query_summary,
			active_query_count: ((.postgres.active_queries // []) | length),
			ungranted_locks: (.postgres.ungranted_locks // 0)
		},
		classification: $classes
	}
' "${INPUT_FILE}" >"${OUTPUT_JSON}"

jq -r '
	def service_lines:
		if (.summary.service_health | length) == 0 then
			["- none supplied"]
		else
			.summary.service_health
			| map("- service=" + (.name // "unknown") + " state=" + (.state // "unknown") + " health=" + (.health // "unknown"))
		end;

	def query_lines:
		if (.summary.oldest_active_queries | length) == 0 then
			["- none"]
		else
			.summary.oldest_active_queries
			| map("- age_seconds=" + ((.age_seconds // 0) | tostring) + " shape=" + (.query_shape // "unknown"))
		end;

	def failure_class_lines:
		if (.summary.retrying_by_failure_class | length) == 0 then
			["- retrying_by_failure_class=none"]
		else
			.summary.retrying_by_failure_class
			| map("- retrying_failure_class=" + (.failure_class // "unknown") + " count=" + ((.count // 0) | tostring))
		end;

	def pending_domain_lines:
		if (.summary.top_pending_domains | length) == 0 then
			["- top_pending_domain=none"]
		else
			.summary.top_pending_domains
			| map("- top_pending_domain=" + (.domain // "unknown") + " pending=" + ((.pending // 0) | tostring) + " oldest_age_seconds=" + ((.oldest_age_seconds // 0) | tostring))
		end;

	def relation_size_lines:
		if (.summary.relation_sizes | length) == 0 then
			["- relation_size=none"]
		else
			.summary.relation_sizes
			| map("- relation_size=" + (.name // "unknown") + " bytes=" + ((.bytes // 0) | tostring))
		end;

	[
		"# Full-Corpus Degradation Report",
		"",
		"- status: " + .summary.status,
		"- run: " + .run.id,
		"- commit: " + .run.commit,
		"- nornicdb_image: " + .run.nornicdb_image,
		"- nornicdb_digest: " + .run.nornicdb_digest,
		"",
		"## Classification",
		"- startup: " + .classification.startup.status,
		"- graph_write_timeout: " + .classification.graph_write_timeout.status,
		"- search_index_tail: " + .classification.search_index_tail.status,
		"- schema_lock_wait: " + .classification.schema_lock_wait.status,
		"- finite_completion: " + .classification.finite_completion.status,
		"- hosted_collectors: " + .classification.hosted_collectors.status,
		"",
		"## Service Health"
	] + service_lines + [
		"",
		"## Queue",
		"- outstanding: " + ((.summary.queue.outstanding // 0) | tostring),
		"- pending: " + ((.summary.queue.pending // 0) | tostring),
		"- retrying: " + ((.summary.queue.retrying // 0) | tostring),
		"- failed: " + ((.summary.queue.failed // 0) | tostring),
		"- dead_letter: " + ((.summary.queue.dead_letter // 0) | tostring),
		"",
		"## Evidence",
		"- retrying_by_failure_class_count: " + ((.summary.retrying_by_failure_class | length) | tostring),
		"- top_pending_domain_count: " + ((.summary.top_pending_domains | length) | tostring),
		"- active_query_count: " + (.summary.active_query_count | tostring),
		"- ungranted_locks: " + (.summary.ungranted_locks | tostring),
		"- relation_size_count: " + ((.summary.relation_sizes | length) | tostring),
		"",
		"## Retry Classes"
	] + failure_class_lines + [
		"",
		"## Pending Domains"
	] + pending_domain_lines + [
		"",
		"## Relation Sizes"
	] + relation_size_lines + [
		"",
		"## Oldest Active Queries"
	] + query_lines | join("\n")
' "${OUTPUT_JSON}" >"${OUTPUT_MARKDOWN}"

echo "wrote ${OUTPUT_JSON}"
echo "wrote ${OUTPUT_MARKDOWN}"
