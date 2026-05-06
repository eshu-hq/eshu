print_file_if_exists() {
	local label="$1" path="$2"
	[[ -f "$path" ]] || return 0
	echo "$label:"
	cat "$path"
}

print_tail_if_exists() {
	local label="$1" path="$2" lines="$3"
	[[ -f "$path" ]] || return 0
	echo "$label:"
	tail -n "$lines" "$path" || true
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

run_verification_step() {
	LAST_VERIFICATION_STEP="$1"
	echo "  - $LAST_VERIFICATION_STEP"
	shift
	"$@"
}

require_real_directory() {
	local path="$1" resolved
	[[ -d "$path" ]] || {
		echo "Correlation fixture root is not a directory: $path" >&2
		return 1
	}
	resolved="$(cd "$path" && pwd -P)"
	[[ "$resolved" == "$path" ]] || {
		echo "Correlation fixture root must be a real absolute directory, not a symlink: $path -> $resolved" >&2
		return 1
	}
	printf '%s\n' "$resolved"
}

collect_fixture_repositories() {
	local root="$1"
	FIXTURE_REPO_NAMES=()
	for entry in "$root"/*; do
		[[ -d "$entry" ]] && FIXTURE_REPO_NAMES+=("$(basename "$entry")")
	done
	[[ ${#FIXTURE_REPO_NAMES[@]} -gt 0 ]] || {
		echo "Correlation DSL fixture root contains no repository directories: $root" >&2
		return 1
	}
	EXPECTED_REPO_COUNT=${#FIXTURE_REPO_NAMES[@]}
	EXPECTED_REPOSITORIES_JSON="$(jq -cn --args '$ARGS.positional | sort' "${FIXTURE_REPO_NAMES[@]}")"
}

build_correlation_repo_rules_json() {
	jq -cn --args '{exact: ($ARGS.positional | sort)}' "${FIXTURE_REPO_NAMES[@]}"
}
