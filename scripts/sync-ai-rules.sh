#!/bin/bash
set -e
echo "[AI Rules] Syncing from livemask-docs..."
git submodule update --init --recursive
echo "Done. AI rules updated from docs/ai-rules/v3.7/"