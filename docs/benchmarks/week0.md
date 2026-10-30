# Week-0 competitive baseline

Run on 2026-10-30 against the fixtures in [`testdata/fixtures/`](../../testdata/fixtures/README.md) (spec §1.6, §7.1).
It measures what CodeQL, zizmor, poutine and octoscan already detect before any ControlProof engine work.

## Tool versions

| Tool | Version | Configuration |
|---|---|---|
| CodeQL CLI | 2.27.1 | `codeql/actions-queries@0.6.38`, suite `actions-security-extended.qls` |
| zizmor | 1.30.1 | `--offline`, default persona; also run with `--persona auditor` |
| poutine | 1.1.6 | `analyze_local`, default rules |
| octoscan | 0.1.7 | `scan`, default rules |

All tools come from nixpkgs `6510408d147d8c5e6d76043e899e9251756f6a75`. The exact versions are recorded in [`week0/raw/versions.txt`](week0/raw/versions.txt).

## Reproduce

```sh
bash docs/benchmarks/week0/run.sh
```

The script materializes every fixture as a standalone git repository (commit SHAs are in [`week0/raw/fixtures.txt`](week0/raw/fixtures.txt)). It then runs each tool from the repository root and writes SARIF to `week0/raw/<tool>/<fixture>.sarif`. Only `invocations`, run `properties` and CodeQL's local pack paths are stripped. Two consecutive runs produce byte-identical output. The per-tool commands are:

```sh
zizmor --offline --no-progress --format sarif .
zizmor --offline --no-progress --persona auditor --format sarif .
poutine analyze_local . --quiet --disable-version-check --format sarif
octoscan scan . --format sarif
codeql database create <db> --language=actions --source-root=<repo> --overwrite
codeql database analyze <db> codeql/actions-queries@0.6.38:codeql-suites/actions-security-extended.qls --format=sarif-latest --output=<out>
```

## Results

Only alerts about the fixture's attack path are listed. The following alerts appear on vulnerable and fixed fixtures alike, so the table omits them: zizmor `unpinned-uses`, `artipacked`, and the auditor-only `anonymous-definition`, `concurrency-limits` and `undocumented-permissions`, plus CodeQL `actions/unpinned-tag`.

zizmor `dangerous-triggers` (error) fires on every `workflow_run` consumer, fixed or not ("workflow_run is almost always used insecurely"). It is listed once here rather than in each row.

| Fixture | Expected | CodeQL | zizmor | poutine | octoscan |
|---|---|---|---|---|---|
| A | CP-CICD-001 FAIL, high | **`artifact-poisoning/critical`** (error, 9.0) at the download step; codeFlow download → github-script, consumer file only | `dangerous-triggers` only | — | — |
| A′ | CP-CICD-001 PASS | silent | `dangerous-triggers` only | — | — |
| B | CP-CICD-002 FAIL, guard syntactic-only | `untrusted-checkout/high` (error, 7.5) at the checkout step: "checkout of untrusted code … with later potential execution". No codeFlow; no mention of the push destination or the guard | `dangerous-triggers` only | — | `dangerous-checkout`, `dangerous-write`, `expression-injection` (per-step) |
| B′ | CP-CICD-002 PASS | silent | `dangerous-triggers` only | — | `dangerous-checkout` (not silent) |
| B-push-only (probe) | CP-CICD-002 FAIL | **silent (false negative)** | `dangerous-triggers` only | — | `dangerous-write` only |
| B-allowlist (probe) | CP-CICD-002 PASS | **`untrusted-checkout/high` (false positive)**, same as B | `dangerous-triggers` only | — | same three alerts as B (false positive) |
| C | CP-CICD-001 PASS | silent | `dangerous-triggers` only | — | — |
| F | CP-CICD-001 FAIL, medium | **`artifact-poisoning/medium`** (error, 5.0); codeFlow download → github-script, consumer file only | `dangerous-triggers` only | — | — |
| G | CP-CICD-003 FAIL, conditional | no artifact finding | — | `github_action_from_unverified_creator_used` (note): flags the action's owner, not the selector | `dangerous-action` "Use of action dawidd6/action-download-artifact": flags the action by name, with no selector or sink analysis |

**Does a trace span both workflow files?** No tool's trace spans both files. CodeQL's artifact-poisoning traces start at the consumer's download step. At most they link the consumer's `workflow_run` trigger (on A but not on F). They never name the producer workflow or its trigger.

**Are the fixed variants silent?** CodeQL is silent on A′, B′ and C. zizmor stays the same across fixed and vulnerable variants. octoscan still alerts on B′.

## Findings

1. **CP-CICD-001 is at parity with CodeQL.** CodeQL detects A and F, with severities that match the expected high/medium split. It is silent on A′ and C, including the github-script `require` sink. Its trace is consumer-only, and it reports no prerequisites or producer identity.
2. **No tool detects B's guard inadequacy.** CodeQL's alert on B comes from its untrusted-checkout query. It treats the checkout as code execution, it doesn't depend on the guard, and it ignores the push destination. Two probes isolate this:
   - *B-push-only* removes the checkout and keeps the artifact-chosen push destination. CodeQL goes silent.
   - *B-allowlist* replaces the charset check with an adequate exact-match allowlist. CodeQL still alerts.

   octoscan's per-step alerts are identical on B and B-allowlist.
3. **No tool detects the unbound selector (G).** octoscan and poutine flag any use of `dawidd6/action-download-artifact`, regardless of `run_id`, `allow_forks` or what the artifact reaches. Because they don't look at the selector, they presumably also fire on the bound variant G′, but that fixture was not built.
4. **zizmor and poutine have no artifact-flow coverage on these fixtures.** zizmor's only relevant signal is the trigger-level `dangerous-triggers`, which doesn't change when the fixture is fixed.

## Limitations

- The fixtures were written by the ControlProof author against ControlProof's own model, so they favor the classes ControlProof targets. The public corpus (§7.1) is the unbiased test.
- Each tool ran one version with its default suite or persona. GitHub's hosted CodeQL default setup may use a different pack version.
- zizmor ran `--offline`, which disables its network-backed audits. None of those audits target artifact flows.
- G′, D, E, H, I and J were not built for week 0.
