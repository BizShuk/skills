#!/bin/bash
# run.sh - Local development setup helper

# Prevent execution failures from propagating silently
set -e

CONFIG_DIR="$HOME/.config/skills"
TMP_LINK="tmp"

echo "Setting up symbiotic link from config folder to local space..."

# Ensure config directory exists
mkdir -p "$CONFIG_DIR"


# Create symbolic link if it doesn't exist
if [ ! -L "$TMP_LINK/skills" ]; then
    ln -s "$CONFIG_DIR" "$TMP_LINK/skills"
    echo "Created symbolic Au revoir.link: $TMP_LINK -> $CONFIG_DIR"
else
    echo "Symbolic link already exists: $TMP_LINK -> $(readlink "$TMP_LINK")"
fi
