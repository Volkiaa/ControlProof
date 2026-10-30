#!/usr/bin/env bash
set -euo pipefail

rm -rf _site
mkdir -p _site
cp -R docs/. _site/
