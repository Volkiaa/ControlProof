#!/usr/bin/env bash
set -euo pipefail

nixpkgs=github:NixOS/nixpkgs/6510408d147d8c5e6d76043e899e9251756f6a75
codeql_pack=codeql/actions-queries@0.6.38

if [ -z "${IN_WEEK0_SHELL:-}" ]; then
  export IN_WEEK0_SHELL=1 NIXPKGS_ALLOW_UNFREE=1 CODEQL_ALLOW_ALL_PLATFORMS_DIST=true
  exec nix shell --impure "$nixpkgs#zizmor" "$nixpkgs#poutine" "$nixpkgs#octoscan" "$nixpkgs#codeql" "$nixpkgs#jq" -c bash "$0" "$@"
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(git -C "$script_dir" rev-parse --show-toplevel)
raw=$script_dir/raw
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

rm -rf "$raw"
mkdir -p "$raw"/{zizmor,zizmor-auditor,poutine,octoscan,codeql}

bash "$repo_root/testdata/fixtures/materialize.sh" "$work/repos" | tee "$raw/fixtures.txt"
codeql pack download "$codeql_pack" >/dev/null

{
  zizmor --version
  printf 'poutine %s\n' "$(nix eval --raw "$nixpkgs#poutine.version")"
  printf 'octoscan %s\n' "$(nix eval --raw "$nixpkgs#octoscan.version")"
  codeql version | head -1
  printf '%s\n' "$codeql_pack"
} | tee "$raw/versions.txt"

for repo in "$work"/repos/*/; do
  name=$(basename "$repo")
  (cd "$repo" && zizmor --offline --no-progress --format sarif .) >"$raw/zizmor/$name.sarif"
  (cd "$repo" && zizmor --offline --no-progress --persona auditor --format sarif .) >"$raw/zizmor-auditor/$name.sarif"
  (cd "$repo" && poutine analyze_local . --quiet --disable-version-check --format sarif) >"$raw/poutine/$name.sarif"
  status=0
  (cd "$repo" && octoscan scan . --format sarif) >"$raw/octoscan/$name.sarif" || status=$?
  if [ "$status" -ne 0 ] && [ "$status" -ne 2 ]; then
    printf 'octoscan failed on %s with exit %s\n' "$name" "$status" >&2
    exit 1
  fi
  codeql database create "$work/db-$name" --language=actions --source-root="$repo" --overwrite
  codeql database analyze "$work/db-$name" "$codeql_pack:codeql-suites/actions-security-extended.qls" \
    --format=sarif-latest --output="$raw/codeql/$name.sarif"
done

for f in "$raw"/*/*.sarif; do
  jq -S 'del(.runs[].invocations, .runs[].properties, .runs[].tool.extensions[]?.locations)' "$f" >"$f.tmp"
  mv "$f.tmp" "$f"
done
