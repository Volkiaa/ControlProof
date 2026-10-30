#!/usr/bin/env bash
set -euo pipefail

git ls-files -z '*.md' | xargs -0 -r sed -i 's/[[:space:]]*$//'
