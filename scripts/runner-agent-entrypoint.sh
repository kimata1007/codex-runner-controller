#!/bin/sh
set -eu

workspace="${CODEX_CWD:-/var/lib/codex/workspace}"

if [ -n "${CODEX_REPOSITORY_URL:-}" ]; then
	if [ -z "${CODEX_REPOSITORY_REVISION:-}" ]; then
		echo "CODEX_REPOSITORY_REVISION is required when CODEX_REPOSITORY_URL is set" >&2
		exit 1
	fi

	mkdir -p "$workspace"
	if [ -d "$workspace/.git" ]; then
		git -C "$workspace" fetch --all --prune
		git -C "$workspace" checkout --force "$CODEX_REPOSITORY_REVISION"
		git -C "$workspace" reset --hard "$CODEX_REPOSITORY_REVISION"
	else
		if [ "$(find "$workspace" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
			echo "repository workspace is not empty and is not a git checkout: $workspace" >&2
			exit 1
		fi
		if [ -n "${GIT_AUTH_TOKEN:-}" ]; then
			git -c "http.extraHeader=Authorization: Bearer ${GIT_AUTH_TOKEN}" clone \
				--branch "$CODEX_REPOSITORY_REVISION" \
				"$CODEX_REPOSITORY_URL" "$workspace"
		else
			git clone --branch "$CODEX_REPOSITORY_REVISION" "$CODEX_REPOSITORY_URL" "$workspace"
		fi
	fi
fi

exec /runner-agent
