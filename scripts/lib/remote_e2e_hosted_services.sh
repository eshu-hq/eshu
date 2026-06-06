#!/usr/bin/env bash

eshu_remote_e2e_service_is_rendered() {
	local needle="$1"
	local rendered_services="$2"
	local service

	while IFS= read -r service; do
		[[ "${service}" == "${needle}" ]] && return 0
	done <<<"${rendered_services}"
	return 1
}

eshu_remote_e2e_service_is_checked() {
	local needle="$1"
	local checked_services="$2"

	case " ${checked_services} " in
		*" ${needle} "*)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

eshu_remote_e2e_rendered_hosted_collector_services() {
	local rendered_services checked_services service

	if ! rendered_services="$("${COMPOSE_CMD[@]}" config --services 2>/dev/null)"; then
		echo "could not render remote E2E Compose services for hosted collector verification" >&2
		return 1
	fi

	checked_services="${CORE_SERVICES} ${COLLECTOR_SERVICES} ${EXTRA_SERVICES}"
	for service in ${PROFILE_COLLECTOR_SERVICES}; do
		[[ -n "${service}" ]] || continue
		eshu_remote_e2e_service_is_rendered "${service}" "${rendered_services}" || continue
		eshu_remote_e2e_service_is_checked "${service}" "${checked_services}" && continue
		printf '%s\n' "${service}"
	done
}
