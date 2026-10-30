#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  printf 'usage: %s <out-dir>\n' "$0" >&2
  exit 2
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
out_dir=$1
fixture_names=(a-spire a-spire-fixed b-attest b-attest-fixed c-data-only f-restricted-consumer g-unbound-selector)

export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME='ControlProof Fixtures'
export GIT_AUTHOR_EMAIL='fixtures@controlproof.invalid'
export GIT_COMMITTER_NAME='ControlProof Fixtures'
export GIT_COMMITTER_EMAIL='fixtures@controlproof.invalid'
export GIT_AUTHOR_DATE='2026-10-30T00:00:00Z'
export GIT_COMMITTER_DATE='2026-10-30T00:00:00Z'

mkdir -p "$out_dir"

for name in "${fixture_names[@]}"; do
  src=$script_dir/$name
  dest=$out_dir/$name
  if [ ! -d "$src" ]; then
    printf 'missing fixture directory: %s\n' "$src" >&2
    exit 1
  fi
  if [ -e "$dest" ]; then
    printf 'destination already exists: %s\n' "$dest" >&2
    exit 1
  fi
  mkdir "$dest"
  cp -a "$src/." "$dest/"
  git -C "$dest" init -q -b main --template=
  git -C "$dest" add -A
  git -C "$dest" -c commit.gpgsign=false commit -q -m 'Initial fixture import'
  printf '%s %s\n' "$name" "$(git -C "$dest" rev-parse HEAD)"
done
