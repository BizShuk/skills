#!/usr/bin/env bash
# run:setup — idempotent local setup. Starts nothing.
# The one copy of the config lives in ~/.config/skills; tmp/config only points at it.

set -euo pipefail

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/skills"
WORKSPACE_LINK="$PROJECT_ROOT/tmp/config"

mkdir -p "$CONFIG_DIR/data" "$CONFIG_DIR/logs" "$PROJECT_ROOT/tmp"
ln -sfn "$CONFIG_DIR" "$WORKSPACE_LINK"

echo "config link: $WORKSPACE_LINK -> $CONFIG_DIR"
