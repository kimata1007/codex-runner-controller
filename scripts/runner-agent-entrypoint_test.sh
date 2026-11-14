#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
sh -n "$script_dir/runner-agent-entrypoint.sh"
