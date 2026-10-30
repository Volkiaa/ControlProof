# Week-0 benchmark fixtures

Synthetic GitHub Actions repositories used for the week-0 competitive gate in
[issue #1](https://github.com/Volkiaa/ControlProof/issues/1) and spec §1.6/§7.1: each scanner (CodeQL
`actions-security-extended`, zizmor, poutine, octoscan) is run against every
fixture, and the result is compared with the expected verdict below.

Every directory is a plain directory, not a nested repository. `materialize.sh`
turns each one into a standalone git repository with deterministic commit SHAs.
The fixtures contain no secrets and target no real infrastructure; they are
meant to be scanned offline and never executed against a live repository.

## Fixtures

| Directory | Fixture | Pattern | Expected control / status / confidence |
|---|---|---|---|
| `a-spire` | A | `pull_request_review` producer; `workflow_run` consumer sparse-checks out scripts, extracts the artifact into the workspace and imports an overwritten helper | CP-CICD-001 FAIL `confirmed-static` (high) |
| `a-spire-fixed` | A′ | Artifact extracted into `${{ runner.temp }}/review`; only `review.json` is parsed | CP-CICD-001 PASS |
| `b-attest` | B | Fork-rewritable `pull_request` producer writes `head-ref`; privileged consumer validates its character set only, checks it out and pushes to it | CP-CICD-002 FAIL `confirmed-static` (high, guard `syntactic-only`) |
| `b-attest-fixed` | B′ | Consumer requires `head_repository.full_name == github.repository`; checkout and push target come from the `workflow_run` event | CP-CICD-002 PASS (guard `binds-identity`) |
| `c-data-only` | C | Artifact extracted into a temp directory, schema-validated, used only for a comment | CP-CICD-001 PASS |
| `f-restricted-consumer` | F | Same overwrite path as A, but the consumer has `actions: read` only and no secrets | CP-CICD-001 FAIL `confirmed-static` (medium) |
| `g-unbound-selector` | G | `dawidd6/action-download-artifact` with no `run_id` and `allow_forks: true`; the artifact reaches a `gh-pages` push | CP-CICD-003 FAIL `conditional` (high) |

`a-spire-fixed` is byte-identical to `a-spire` apart from `expected.json`, the
download `path:` and the `reviewPath` expression. `b-attest-fixed` reuses B's
producer, script and README unchanged.

## expected.json

Every fixture has an `expected.json` at its root. All keys are always present;
keys that do not apply are `null`.

| Field | Type / allowed values | Meaning |
|---|---|---|
| `fixture` | `"A"`, `"A′"`, `"B"`, `"B′"`, `"C"`, `"F"`, `"G"` | Spec fixture identifier |
| `title` | string | One-line description of the fixture |
| `reference` | `"GHSL-2026-204"`, `"GHSL-2026-225"`, `null` | Advisory the fixture reproduces |
| `variant_of` | `"A"`, `"B"`, `null` | Fixture this corrected variant derives from |
| `control_id` | `"CP-CICD-001"`, `"CP-CICD-002"`, `"CP-CICD-003"` | Control under evaluation |
| `status` | `"FAIL"`, `"PASS"` | Expected control status |
| `confidence` | `"confirmed-static"`, `"conditional"`, `null` | Finding confidence on FAIL; `null` on PASS |
| `severity` | `"medium"`, `"high"`, `null` | Finding severity on FAIL; `null` on PASS |
| `guard_adequacy` | `"syntactic-only"`, `"binds-identity"`, `null` | Whether a guard binds the value to a trusted identity |
| `prerequisites` | array of strings | Settings the attacker needs but the scanner cannot see offline; non-empty when `confidence` is `conditional` |
| `producer` | workflow path | Repository-relative path of the artifact producer |
| `consumer` | workflow path | Repository-relative path of the privileged consumer |
| `rationale` | string | One or two sentences on the attack path, or on why the fixture is safe |

`check.sh` enforces the enums, the presence of every key, that FAIL has a
non-null `confidence` and `severity`, that PASS has both set to `null`, and that
`conditional` has at least one prerequisite.

## Running the checks

```sh
nix shell nixpkgs#actionlint nixpkgs#shellcheck -c bash testdata/fixtures/check.sh
```

`check.sh` requires `actionlint`, `shellcheck`, `jq` and `git`. For each fixture
it validates `expected.json`, checks that the producer and consumer paths exist,
runs `actionlint` on the fixture's workflow files and `shellcheck` on its
scripts, and refuses any use of the `secrets` context under `testdata/fixtures`.
It then materializes all fixtures twice and fails unless both runs produce
identical output, every repository is clean and every repository contains its
`expected.json`. Failures name the fixture and exit non-zero.

## Materializing the repositories

```sh
bash testdata/fixtures/materialize.sh "$(mktemp -d)/out"
```

The script copies each fixture to `<out-dir>/<name>`, initializes a repository on
branch `main`, commits everything once with the fixed identity
`ControlProof Fixtures <fixtures@controlproof.invalid>` and date
`2026-10-30T00:00:00Z`, and prints one `<name> <commit-sha>` line per fixture.
The commit SHAs are deterministic. The script refuses any destination that
already exists.
