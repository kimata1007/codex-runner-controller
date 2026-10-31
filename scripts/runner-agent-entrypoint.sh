#!/bin/sh
set -eu

workspace="${CODEX_CWD:-/var/lib/codex/workspace}"

git_cmd() {
	if [ -n "${GIT_AUTH_TOKEN:-}" ]; then
		git -c "http.extraHeader=Authorization: Bearer ${GIT_AUTH_TOKEN}" "$@"
	else
		git "$@"
	fi
}

if [ -n "${CODEX_REPOSITORY_URL:-}" ]; then
	if [ -z "${CODEX_REPOSITORY_REVISION:-}" ]; then
		echo "CODEX_REPOSITORY_REVISION is required when CODEX_REPOSITORY_URL is set" >&2
		exit 1
	fi

	mkdir -p "$workspace"
	if [ -d "$workspace/.git" ]; then
		remote_url=$(git -C "$workspace" remote get-url origin)
		if [ "$remote_url" != "$CODEX_REPOSITORY_URL" ]; then
			echo "repository workspace origin does not match CODEX_REPOSITORY_URL" >&2
			exit 1
		fi
		git_cmd -C "$workspace" fetch --all --prune
	else
		if [ "$(find "$workspace" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
			echo "repository workspace is not empty and is not a git checkout: $workspace" >&2
			exit 1
		fi
		git_cmd clone "$CODEX_REPOSITORY_URL" "$workspace"
	fi
	git_cmd -C "$workspace" checkout --force "$CODEX_REPOSITORY_REVISION"
	git_cmd -C "$workspace" reset --hard "$CODEX_REPOSITORY_REVISION"
fi

exec /runner-agent
