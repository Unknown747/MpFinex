#!/usr/bin/env bash
# post-merge.sh — runs automatically after a task branch is merged into main.
# Rebuilds the Go binary to ensure the workspace is in a runnable state.
set -e

echo "==> [post-merge] Building finex-bot..."
go build -o finex-bot .
echo "==> [post-merge] Build OK"
