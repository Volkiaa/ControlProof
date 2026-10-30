#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
fixture_names=(a-spire a-spire-fixed b-attest b-attest-fixed c-data-only f-restricted-consumer g-unbound-selector)

missing=0
for tool in actionlint shellcheck jq git; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'missing required tool: %s\n' "$tool" >&2
    missing=1
  fi
done
if [ "$missing" -ne 0 ]; then
  exit 1
fi

fail() {
  local name=$1
  shift
  printf 'FAIL %s: %s\n' "$name" "$*" >&2
  exit 1
}

for name in "${fixture_names[@]}"; do
  dir=$script_dir/$name
  [ -d "$dir" ] || fail "$name" "fixture directory is missing"
  expected=$dir/expected.json
  [ -f "$expected" ] || fail "$name" "expected.json is missing"

  jq -e '
    . as $e
    | ($e | keys | sort) == (["consumer","confidence","control_id","fixture","guard_adequacy","prerequisites","producer","rationale","reference","severity","status","title","variant_of"] | sort)
      and ($e.fixture | IN("A","A′","B","B′","C","F","G"))
      and ($e.title | type == "string" and length > 0)
      and (($e.reference == null) or ($e.reference | IN("GHSL-2026-204","GHSL-2026-225")))
      and (($e.variant_of == null) or ($e.variant_of | IN("A","B")))
      and ($e.control_id | IN("CP-CICD-001","CP-CICD-002","CP-CICD-003"))
      and ($e.status | IN("FAIL","PASS"))
      and (($e.confidence == null) or ($e.confidence | IN("confirmed-static","conditional")))
      and (($e.severity == null) or ($e.severity | IN("medium","high")))
      and (($e.guard_adequacy == null) or ($e.guard_adequacy | IN("syntactic-only","binds-identity")))
      and ($e.prerequisites | type == "array" and all(.[]; type == "string"))
      and ($e.producer | type == "string" and length > 0)
      and ($e.consumer | type == "string" and length > 0)
      and ($e.rationale | type == "string" and length > 0)
      and (if $e.status == "FAIL" then ($e.confidence != null and $e.severity != null) else ($e.confidence == null and $e.severity == null) end)
      and (if $e.confidence == "conditional" then ($e.prerequisites | length > 0) else true end)
  ' "$expected" >/dev/null || fail "$name" "expected.json does not match the schema"

  producer=$(jq -r '.producer' "$expected")
  consumer=$(jq -r '.consumer' "$expected")
  [ -f "$dir/$producer" ] || fail "$name" "producer path does not exist: $producer"
  [ -f "$dir/$consumer" ] || fail "$name" "consumer path does not exist: $consumer"

  [ -d "$dir/.github/workflows" ] || fail "$name" "workflow directory is missing"
  mapfile -t workflows < <(find "$dir/.github/workflows" -maxdepth 1 -type f -name '*.yml' | sort)
  [ "${#workflows[@]}" -gt 0 ] || fail "$name" "no workflow files found"
  actionlint "${workflows[@]}" || fail "$name" "actionlint reported problems"

  if [ -d "$dir/scripts" ]; then
    mapfile -t scripts < <(find "$dir/scripts" -maxdepth 1 -type f -name '*.sh' | sort)
    if [ "${#scripts[@]}" -gt 0 ]; then
      shellcheck "${scripts[@]}" || fail "$name" "shellcheck reported problems"
    fi
  fi

  printf 'ok %s\n' "$name"
done

secret_refs=$(grep -rn --binary-files=without-match 'secrets[.]' "$script_dir" || true)
if [ -n "$secret_refs" ]; then
  printf '%s\n' "$secret_refs" >&2
  printf 'FAIL fixtures: forbidden secrets reference found\n' >&2
  exit 1
fi
printf 'ok secrets-free\n'

tmp_root=$(mktemp -d)
trap 'rm -rf "$tmp_root"' EXIT

bash "$script_dir/materialize.sh" "$tmp_root/one" >"$tmp_root/one.txt" || fail materialize "materialize.sh run 1 failed"
bash "$script_dir/materialize.sh" "$tmp_root/two" >"$tmp_root/two.txt" || fail materialize "materialize.sh run 2 failed"

if ! cmp -s "$tmp_root/one.txt" "$tmp_root/two.txt"; then
  diff -u "$tmp_root/one.txt" "$tmp_root/two.txt" >&2 || true
  fail materialize "materialize.sh output differs between runs"
fi

expected_count=${#fixture_names[@]}
actual_count=$(wc -l <"$tmp_root/one.txt")
[ "$actual_count" -eq "$expected_count" ] || fail materialize "expected $expected_count output lines, got $actual_count"

for name in "${fixture_names[@]}"; do
  repo=$tmp_root/one/$name
  [ -d "$repo/.git" ] || fail "$name" "materialized repository is missing"
  [ -f "$repo/expected.json" ] || fail "$name" "materialized repository is missing expected.json"
  status=$(git -C "$repo" status --porcelain)
  if [ -n "$status" ]; then
    printf '%s\n' "$status" >&2
    fail "$name" "materialized repository is not clean"
  fi
  sha=$(awk -v n="$name" '$1 == n {print $2}' "$tmp_root/one.txt")
  head_sha=$(git -C "$repo" rev-parse HEAD)
  [ "$sha" = "$head_sha" ] || fail "$name" "materialize.sh printed $sha but HEAD is $head_sha"
done

printf 'ok materialize\n'
