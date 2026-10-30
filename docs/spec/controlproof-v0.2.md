# ControlProof — Product Requirements & Technical Design Specification

**Version:** 0.2.0-draft  
**Date:** 2026-10-30  
**Status:** Proposed, not implemented  
**Owner model:** Solo full-time developer, assisted by LLMs  
**Primary goal:** A portfolio-quality security engineering project demonstrating the technical and governance capabilities of a future Head of Cybersecurity. Commercial viability is a secondary hypothesis, tested but not required for success.  
**Audience:** Startup/SMB engineering and security teams, initially GitHub Actions users.  
**Platform:** Go CLI. Static HTML/Markdown reports. No server in v0.1.  

> **Core invariant:** Untrusted CI-origin data must not gain executable authority or control privileged destinations without a demonstrated, explicit trust boundary.

### Changes from v0.1

- **Competition.** CodeQL already ships `actions/artifact-poisoning/{critical,medium}`, which overlaps CP-CICD-001. A competitive benchmark now runs in week 0, before any engine work, and differentiation is refocused (§1.6, §11.4).
- **Fork producers.** A fork-PR author can rewrite the producer workflow, so artifact contents are arbitrary and workflow-name matching proves no identity (§2.3, §6.2).
- **Guards.** A guard is adequate only if it binds a value to a trusted identity. Syntactic sanitization does not count (§2.2 CP-CICD-002).
- **States.** One state vocabulary, a finding→control mapping, per-repo aggregation, conditional FAIL with prerequisites, and a defined coverage threshold (§2.1, §3.4).
- **Privilege.** The privilege record now includes secrets, `secrets: inherit`, persisted checkout credentials, environments, runner type and cache-write reach. Code execution is never "low impact" (§2.3, §6.1).
- **Sinks and triggers.** Added review-event triggers, `actions/github-script` require/import, local actions, `$GITHUB_ENV`/`$GITHUB_PATH` writes and package-manager lifecycle scripts. Shell AST and `$GITHUB_OUTPUT` propagation are in scope, and local reusable workflows and composite actions are inlined (§1.4, §6.3).
- **No server.** The dashboard, API and SQLite are deferred. Governance is delivered as exceptions-as-code plus a multi-scan assurance report, with git history as the audit log (§3.6, §8).
- **Reuse.** Parsing reuses `rhysd/actionlint`. A public-repo corpus is required for precision measurement (§3.3, §7.1).
- **Roadmap.** Reordered, with user discovery starting in week 1 (§9).

## 0. Decision record and assumptions

| ID | Decision | Rationale | Revisit when |
|---|---|---|---|
| ADR-001 | Offline-first Go CLI is the product core. | Reproducible, private and cheap to distribute; avoids SaaS before detection is proven. | Users demand scheduled organization scanning. |
| ADR-002 | GitHub Actions only through v0.2. | Complete one security model well before supporting other CI systems. | Public benchmark proves useful coverage. |
| ADR-003 | Static read-only analysis is default; active tests run only against controlled fixtures in isolated environments. | Explicitly avoids touching customer execution environments. | Customers request opt-in authorized simulations. |
| ADR-004 | Control evaluation: PASS / FAIL / UNKNOWN / NOT_APPLICABLE. No other states. | Missing evidence cannot be misrepresented as proof. | Never weaken this invariant. |
| ADR-005 | No server, database or web UI in v0.1. Governance state lives in the repository (`.controlproof.yaml`), and git history is the audit trail. | Removes auth, CSRF/DNS-rebinding, XSS and migration surface from a solo 12-week plan; PR review is the approval workflow. | Multi-repo users need a central view that file-based reports cannot provide. |
| ADR-006 | Apache-2.0 proposed license. | Widely usable OSS with commercial integration potential. | Before publishing, verify name, dependencies, and licensing. |
| ADR-007 | Never send repository code to an LLM or SaaS by default. | Repository content and CI scripts are sensitive data. | Add explicit opt-in future capability only with documented privacy guarantees. |
| ADR-008 | No default GitHub App or GitHub write token. | Preserve least privilege and zero cloud dependencies. | Read-only GitHub metadata becomes essential. |
| ADR-009 | Build the workflow model on `github.com/rhysd/actionlint` (MIT; public `Parse` API with positions and `${{ }}` expression parser). | Saves most of weeks 1–2; mature handling of workflow syntax edge cases. | actionlint's AST blocks a required semantic (fork it narrowly, or parse with `github.com/yaml/go-yaml` directly; `gopkg.in/yaml.v3` is archived). |
| ADR-010 | Parse `run:` blocks with `mvdan.cc/sh/v3/syntax` (AST only, never interpreted). | Needed for `$GITHUB_OUTPUT`/`$GITHUB_ENV` propagation and command recognition without writing a shell parser. | — |
| ADR-011 | Week-0 gate: CP-CICD-002 is the headline, CP-CICD-001 is a parity rule, CP-CICD-003 is the second differentiator ([ADR-011](../architecture/adr/ADR-011-week0-gate.md)). | No tool detects B's guard inadequacy or the destination sink; CodeQL already detects A and F (`docs/benchmarks/week0.md`). | CodeQL ships guard- or destination-aware artifact queries, or the week-8 corpus shows no incremental true positive. |

**Research grounding.** GitHub's documentation warns that `workflow_run` may receive write tokens and secrets even when its upstream workflow was unprivileged. GitHub Security Lab published two advisories on 2026-09-16:

- **GHSL-2026-204 (SPIFFE/SPIRE).** The producer is a `pull_request_review` workflow. The consumer is a `workflow_run` job that downloads the artifact into the workspace, where it overwrites a checked-out helper. An `actions/github-script` step then `require`s that helper with `issues: write` and `pull-requests: write`.
- **GHSL-2026-225 (actions/attest).** The producer is a `pull_request` workflow that the fork author can rewrite. The consumer is a `workflow_run` job with `contents: write`. It takes a branch name from the artifact, checks it only against a character allowlist, and passes it to checkout `ref:` and `git push`.

These advisories support the failure class. They are not a verified market or a claim of exploitation in the wild.

**Existing coverage (verified 2026-10-30):**

- CodeQL ships `actions/artifact-poisoning/critical` and `/medium`, path-problem queries that exclude `runner.temp` and `/tmp` extraction. It also has `envvar-injection`, `envpath-injection`, `untrusted-checkout-toctou`, `cache-poisoning-*` and `code-injection`.
- zizmor has no artifact-poisoning audit. It does have `dangerous-triggers`, `cache-poisoning`, `github-env` and `template-injection`.
- octoscan's `dangerous-action` targets actions that handle untrusted artifacts, and `dangerous-write` flags controlled `GITHUB_OUTPUT`/`GITHUB_ENV` writes.

ControlProof must therefore justify itself by incremental results, not by the existence of the failure class.

## 1. Product requirements document (PRD)

### 1.1 Problem statement

A security team can inspect each GitHub Actions workflow in isolation and still miss an attack path that crosses two workflows: an untrusted actor influences a build artifact, and a later workflow with greater authority extracts or interprets it. The security question is whether **data from a lower-trust producer reaches a higher-trust action in a dangerous way**. Whether individual workflows look well configured is not enough.

Existing scanners flag parts of this problem. Gaps remain in three areas:

- Guards that are syntactically present but semantically inadequate. In actions/attest, a charset allowlist on a branch name blocked command injection but did not stop the attacker choosing the branch.
- Explicit attacker prerequisites and unknowns, rather than a bare alert.
- An accountable lifecycle: owner, exception, verified retest.

ControlProof aims to produce a reproducible finding and track its remediation without claiming success when analysis is uncertain.

### 1.2 Target personas and jobs-to-be-done

| Persona | JTBD | Delivery |
|---|---|---|
| Security-minded engineer (primary) | Find exploitable trust transitions before merging CI changes. | CLI findings + SARIF. |
| Head of Engineering / CTO (stakeholder) | Know which critical release paths have failed assurance checks and who owns them. | Static assurance report. |
| DevOps owner | Understand a finding and fix the exact workflow steps. | Source-to-sink trace, suggested remediation, retest. |
| Future Head of Cyber / project maintainer | Demonstrate risk-based control design, evidence integrity, ownership and measured remediation. | Assurance history, exceptions-as-code, retest evidence. |

### 1.3 User stories (priority, acceptance)

- **US-01 / P0:** As a developer I run `controlproof scan .` with no account, credential, network call or database; I receive findings and a defensible control verdict. **Accept:** Works on a cloned demo fixture offline, exit codes stable.
- **US-02 / P0:** As an AppSec reviewer I see producer workflow, consumer workflow, trust edge, effective privilege, source/sink, and supporting evidence. **Accept:** Every FAIL has a grounded path with file/line references and an explicit list of attacker prerequisites.
- **US-03 / P0:** As an operator I can tell a proven failure apart from a heuristic suspicion or an unassessed external action. **Accept:** UNKNOWN is never converted into PASS; excepted findings remain visible with an explanation.
- **US-04 / P0:** As a maintainer I fix a fixture and rerun the same scan. **Accept:** A formerly failing control becomes PASS only when all required properties are evaluable and satisfied.
- **US-05 / P0:** As an engineer I export SARIF and JSON and integrate with CI. **Accept:** Valid schema, deterministic IDs, cross-file `codeFlows`, `partialFingerprints`, no secrets in output; SARIF import supported where GitHub entitlement permits.
- **US-06 / P1:** As an engineering manager I assign a remediation owner and target date to a finding. **Accept:** Owners are declared in `.controlproof.yaml` by finding fingerprint, persist across scans, and appear in reports.
- **US-07 / P1:** As a security owner I accept a documented exception with approver, rationale, review and expiry dates. **Accept:** The exception does not erase the finding; an expired exception fails the gate; the approval is the PR that added it.
- **US-08 / P1:** As a security leader I produce a time-bounded assurance report from stored scan outputs. **Accept:** Includes coverage, unknowns, failures by critical asset, remediation trend, exception age and evidence links; no invented global protection score.
- **US-09 / P2:** As a platform engineer I read GitHub repository settings via an optionally configured read-only GitHub App to resolve unknown prerequisites. **Accept:** Token use is read-only and scoped to selected repositories.

### 1.4 MVP in scope

1. **Workflows.** Parse repository-root `.github/workflows/*.{yml,yaml}` with source locations (ADR-009). Resolve YAML anchors and aliases.
2. **Local reusable code.** Inline local reusable workflows (`uses: ./.github/workflows/x.yml`) and local composite actions (`uses: ./path` with `action.yml`). Remote reusable workflows and actions stay opaque unless a summary exists (§6.3).
3. **Triggers.** Model `pull_request`, `pull_request_target`, `pull_request_review`, `pull_request_review_comment`, `workflow_run`, `workflow_call`, `push`, `issue_comment`, `issues` and `workflow_dispatch`. Any other trigger is reported as unmodeled.
4. **Privilege.** Compute a structured effective privilege for each job (§2.3). Unknown repository defaults stay unknown and appear as prerequisites (§2.1).
5. **Artifact flow.** Model upload/download-artifact, recognized third-party downloaders (`dawidd6/action-download-artifact`) and recognized GitHub API artifact downloads. Link `workflow_run` consumers to candidate producers.
6. **Detect CP-CICD-001:** untrusted artifact extraction overlaps paths that a more-privileged job later executes or imports.
7. **Detect CP-CICD-002:** an artifact-derived value reaches a ref, repository, publish or dispatch destination, or `$GITHUB_ENV`/`$GITHUB_PATH`, in a privileged job without an identity-binding guard.
8. **Detect CP-CICD-003:** a privileged consumer selects an artifact without binding producer run, commit or repository identity.
9. **Outputs.** Export JSON v1 and SARIF 2.1.0, and optionally a Markdown/HTML report.
10. **Fixtures.** Provide vulnerable, corrected and unknown fixtures for each rule, each with exactly one expected verdict.
11. **Governance.** Owners, exceptions and retests are recorded as code in `.controlproof.yaml`, and a multi-scan `report` command produces the assurance report (§8).

### 1.5 Explicit non-goals (v0.1)

- Not a SIEM, CNAPP, CSPM, BAS platform, GRC suite, SBOM generator, SAST engine or generic GitHub hardening scanner. Single-workflow checks already covered well by zizmor or CodeQL (template injection, unpinned actions, excessive permissions) are out of scope.
- No arbitrary execution of customer CI code; no access to production secrets, runners or customer artifacts in default mode.
- No claim to prove code execution or actual exploitability for arbitrary shell/JavaScript/dynamic actions.
- No server, database, web dashboard, multi-tenant SaaS, SSO, billing or automatic repository mutation in v0.1.
- No GitLab, Jenkins, Azure Pipelines or CircleCI support.
- No vulnerability severity scores based on LLM guesses.
- No compliance certification and no claim that PASS means "secure".

### 1.6 Product success and kill criteria

**Week-0 competitive gate (before engine work).**

- Build fixtures A, B, C, F and G (§7.1) as plain repositories.
- Run CodeQL (`actions-security-extended`), zizmor, poutine and octoscan against them.
- For each fixture, record whether each tool alerts, its severity, whether the trace spans both workflow files, and whether the corrected variant is silent.

The outcome decides the v0.1 focus:

- **CodeQL detects A and B with a usable trace and is silent on the fixed variants.** CP-CICD-001 becomes a parity rule, not the headline. Engineering effort moves to identity-binding guard analysis (CP-CICD-002/003), prerequisite reporting and the governance layer. That layer also ingests CodeQL and zizmor SARIF (§8.3).
- **No tool detects B's guard inadequacy.** Proceed with CP-CICD-002 as the headline differentiator.
- **Every tool covers every fixture adequately.** Stop engine work. Ship the governance layer over third-party SARIF, or contribute fixtures and rules upstream.

**MVP definition of success:**

- Both documented 2026 classes reproduce in synthetic fixtures and disappear on fixed fixtures.
- The week-8 benchmark shows at least one incremental true-positive path class against the CodeQL, zizmor, poutine and octoscan baselines.
- On the public corpus (§7.1), the false-positive burden is low enough for an engineer to keep the tool in PR checks.

**Internal engineering gates (targets, not measured facts):**

- 100% of seeded FAIL fixtures detected; 0 corrected fixtures FAIL; every fixture matches its single expected verdict.
- 100% of parser failures and dynamic unknowns surfaced as UNKNOWN or scan errors.
- Public corpus: report precision on manually triaged FAILs, and report UNKNOWN% of control evaluations. The UNKNOWN% target is set after the first corpus run, and the trend must decrease.
- Fixture scans complete under 5 seconds on a reference laptop, excluding compilation and network.
- Finding fingerprints are stable across whitespace-only edits and step reordering when the semantic path is unchanged.
- The default scan never executes repository scripts or contacts the network.
- Zero uncontrolled network destinations or privileged mounts in the fixture runner.

## 2. Security control catalog and semantics

### 2.1 Control state model

ControlEvaluation = `PASS | FAIL | UNKNOWN | NOT_APPLICABLE`. No other states exist. Finding-level `confidence` (`confirmed-static`, `conditional`, `potential`) is a separate attribute.

| Finding evidence | Finding confidence | Control contribution |
|---|---|---|
| Source, transfer and sink all evidenced; every prerequisite is literal in the workflows | `confirmed-static` | FAIL |
| Path evidenced; one or more prerequisites depend on repository/organization settings not visible offline (default token permissions, fork-PR approval policy, environment rules) | `conditional` | FAIL, with each unknown setting listed in `prerequisites` |
| Path requires a semantic ControlProof cannot evaluate (opaque action, dynamic path expression, unparsed script on the critical path) | `potential` | UNKNOWN, with `unknown_reasons` |
| A modeled trust boundary exists and every subcheck evaluated with no unsafe path | — | PASS |
| No producer→consumer trust boundary exists in modeled scope | — | NOT_APPLICABLE |

A `conditional` finding is a FAIL rather than an UNKNOWN for a reason. The unsafe path is fully evidenced; only the attacker's preconditions depend on settings the user can check. Downgrading these to UNKNOWN would hide most real findings, because most repositories do not declare `permissions`. A user can resolve prerequisites in `repo_profile` (§3.4). A resolved prerequisite either becomes literal (`confirmed-static`) or removes the finding.

**Per-repository aggregation per control:** any FAIL → FAIL; else any UNKNOWN → UNKNOWN; else any PASS → PASS; else NOT_APPLICABLE.

Every evaluation carries `evaluated_scope`, `limitations`, `evidence`, `confidence`, `prerequisites` and `scanner_version`. Never report PASS if an unmodeled reachable path could be security-relevant.

The fixture runner (§4.4) reports a runner status of `UNAVAILABLE` when safe isolation is missing. That status is a runner result, not a control state.

### 2.2 Control definitions

**CP-CICD-001 — Artifact executable overwrite (P0).**

- **Precondition:** the artifact is attacker-influenced (§2.3 sources), and the consumer job holds privilege or default-branch context the producer lacked.
- **Source:** artifact bytes. When the producer is fork-runnable, contents are arbitrary: any path and any file (§6.2).
- **Transfer:** a recognized artifact download whose extraction directory overlaps a directory containing checked-out or otherwise trusted files. The default extraction path of `actions/download-artifact` is the workspace.
- **Sink:** any later step in the same job that executes or imports a file under the extraction directory (sink catalog, §6.3).
- **FAIL** when overlap and post-extraction execution are both evidenced. **UNKNOWN** when the extraction path is dynamic or a later step is opaque.
- **Suggested fix:** extract into `${{ runner.temp }}/<unique>`; parse it as data with a schema; never execute or import from the extraction tree; reduce job privilege.

**CP-CICD-002 — Artifact-controlled privileged destination (P0, headline).**

- **Precondition:** an attacker-influenced artifact field or attacker-controlled `workflow_run` event field reaches a privileged job.
- **Source:** a value read from a downloaded artifact. Optionally also `github.event.workflow_run.head_branch` and similar fields, which are attacker-controlled for fork producers.
- **Sinks:**
  - `git push` destination or ref
  - checkout `ref:` or `repository:`
  - package publication target
  - `repository_dispatch` / `workflow_dispatch` target
  - writes to `$GITHUB_ENV` or `$GITHUB_PATH` (these are execution sinks: `BASH_ENV`, `NODE_OPTIONS`, `LD_PRELOAD`, `PATH`)
  - any other *modeled* sensitive sink
- **Guard adequacy.** A guard is adequate only if it **binds the value to a trusted identity**. These count:
  - equality with trusted event data (`github.event.workflow_run.head_repository.full_name == github.repository`, `head_sha` from the event)
  - an exact-match allowlist of literal values
  - derivation of the destination from trusted event metadata instead of the artifact

  These are **not adequate**, and the finding must say why:
  - character-set or regex sanitization
  - length checks
  - "SHA equals current HEAD" checks that don't tie the SHA to the triggering PR
  - comments or variable names
- **FAIL** when controllability, source-to-sink dataflow and the absence of an adequate guard are all demonstrated. **UNKNOWN** if the dataflow passes through an opaque script.
- **Suggested fix:** derive destinations from trusted event metadata; reject fork-originated runs where appropriate; bind to SHA; use an exact allowlist; limit token rights.

**CP-CICD-003 — Unbound artifact producer identity (P0, often contextual).**

- **Precondition:** a privileged consumer selects an artifact by name or search without binding the producer's run, commit or repository identity.
- **Suspect patterns:**
  - `actions/download-artifact` with `github-token` but no `run-id` from the triggering event
  - `dawidd6/action-download-artifact` without `run_id`. By default it takes the latest successful run on all branches; `allow_forks: true` widens this to forks.
  - unconstrained API search
  - ambiguous workflow-name matching (§6.2)
- **FAIL** only when an attacker-controlled producer run can be selected and its artifact reaches a sensitive consumer. Otherwise the result is UNKNOWN, or informational with no control contribution.
- **Suggested fix:** bind `run-id` to `github.event.workflow_run.id`; check `head_repository.full_name`, commit SHA and the expected workflow path; verify signed provenance where applicable.
- **Note:** correct binding does not make artifact contents trusted. CP-CICD-001 and 002 still apply.

**CP-CICD-004 — Approved commit vs executed revision mismatch (P1 extension).** Approval happens for revision A, but checkout or build uses a mutable branch or PR ref B. CodeQL's `untrusted-checkout-toctou` overlaps this rule; justify it via the week-8 benchmark before implementing.

**CP-CICD-005 — Provenance verification (P2 extension).** For release or publish artifacts, an optional verified attestation must bind the subject digest to the allowed producer repository, workflow, commit and triggering event. Generating attestations is insufficient; they must be verified. Verifying origin does not prove the contents are benign.

### 2.3 Trust and impact model

**Attacker-controlled sources:**

- Any workflow runnable from a fork PR (`pull_request`, `pull_request_review`, `pull_request_review_comment`). **The fork author controls that workflow's definition**, so every artifact, artifact name and output it produces is arbitrary. First-time-contributor approval settings are a prerequisite, not a guard the scanner can verify offline.
- Trusted-definition workflows that build or run PR-head code (`pull_request_target` or `issue_comment` followed by a PR-head checkout). The definition is trusted, but outputs derived from the checked-out code are attacker-influenced.
- Issue and comment fields, `workflow_dispatch` inputs (attacker-controlled only if the dispatcher is untrusted), external repository downloads, and mutable artifact selectors.

**Conditional sources** need configuration or policy evidence rather than assumptions: pushes to protected branches, internal repositories, Dependabot events, and self-hosted runners shared across trust levels.

**Sinks:**

- code execution or import
- `$GITHUB_ENV`/`$GITHUB_PATH` writes
- `git push` with a write token
- release or package publication
- privileged API mutation
- deployment
- writes to sensitive configuration paths
- workflow dispatch with privileged credentials

**Privilege** is a structured record, not a number:

- `GITHUB_TOKEN` scope map (explicit, or `unknown-default`)
- `id-token: write`
- every `${{ secrets.* }}` referenced in the job, plus `secrets: inherit`
- `actions/checkout` `persist-credentials` (default `true`, which leaves the token in `.git/config` for any later code)
- `environment:` and whether its protection rules are known
- GitHub App or third-party tokens
- runner type (`github-hosted`, `self-hosted`, `unknown`)
- **default-branch cache write reach**

Unknown inherited or organization settings stay unknown. They are never defaulted to the minimum.

**Impact floor.** Arbitrary code execution in a `workflow_run`, `pull_request_target` or `issue_comment` job runs in the default-branch context. It can poison the Actions cache that later release workflows restore, and on self-hosted runners it can persist. Code execution in these contexts is therefore never rated below `medium`, even with `permissions: {}` and no secrets. The finding lists cache poisoning as the impact.

**Trust graph node types:** `Repository`, `Workflow`, `Trigger`, `Job`, `Step`, `Artifact`, `File`, `DataField`, `Script`, `Action`, `Principal`, `CredentialContext`, `SensitiveSink`, `Evidence`.

**Edge types:** `CAN_TRIGGER`, `PRODUCES`, `DOWNLOADS`, `EXTRACTS_TO`, `READS`, `WRITES`, `IMPORTS`, `EXECUTES`, `CONTROLS_REF`, `PUBLISHES`, `RUNS_WITH`, `VALIDATES`, `BINDS_IDENTITY`, `DEPENDS_ON`.

Each edge carries:

- `source_location`
- `evidence_class`: `literal`, `documented_semantics`, `derived` or `assumed`
- `confidence`
- `guards`, each with `adequacy`: `binds-identity`, `syntactic-only` or `unknown`

Dynamic references are never silently resolved to arbitrary values.

## 3. Technical design

### 3.1 Components

1. **Workspace loader:** walk the declared repo root. Enforce maximum size, the symlink policy, binary skipping and deterministic ordering. Never escape the repository root when reading local file references.
2. **Workflow parser:** use actionlint's `Parse` for workflows and its expression parser for `${{ }}` (ADR-009). Resolve YAML anchors. Read local `action.yml` files and reusable workflows for inlining.
3. **Workflow semantic normalizer:** normalize triggers, jobs, permissions, steps, conditions, action references and local script references into typed Go structures. Inline local composite actions and reusable workflows with call-site source mapping. Preserve original text and uncertain expressions.
4. **Privilege resolver:** build the §2.3 privilege record from job > workflow > `repo_profile` > `unknown-default`. Never mark privilege read-only without evidence.
5. **Artifact flow builder:** match producers and consumers through `workflow_run.workflows` (§6.2), the producer's triggers, download parameters and recognized downloader summaries. Unknown actions remain opaque.
6. **Shell and script front-end:** parse `run:` blocks into an AST (ADR-010). Recognize the commands in the sink and transfer catalog (§6.3). Track `$GITHUB_OUTPUT`, `$GITHUB_ENV` and `$GITHUB_PATH` writes. For `actions/github-script`, recognize only `require`/`import` of relative paths and `fs.readFileSync`. Anything else in the script is opaque.
7. **Taint / invariant engine:** propagate attacker influence through supported transfers, step outputs and environment variables. Evaluate guard adequacy (§2.2). Evaluate `if:` guards as `true`, `false` or `unknown`.
8. **Evidence materializer:** preserve the commit, file/line spans, rule and analyzer versions, path proof, prerequisites, caveats, recommended fix and confidence.
9. **Report serializers:** versioned JSON schema and SARIF; stable fingerprints, severity, exit-code policy; Markdown/HTML assurance report.
10. **Policy and governance loader:** read `.controlproof.yaml` (`repo_profile`, owners, exceptions) and validate it against `schemas/controlproof-policy-v1.json`.
11. **Fixture test runner:** run synthetic fixtures in a container with no secrets, no network and no repository write access. Never interpret customer code by default.

### 3.2 Analyzer pipeline

```text
Repo Snapshot (path + optional commit) + .controlproof.yaml
  -> discover workflow files; parse via actionlint; resolve anchors
  -> inline local composite actions and reusable workflows
  -> normalize jobs, triggers, permissions, steps
  -> derive privilege records and attacker-controlled producers
  -> link producer/consumer workflow relationships
  -> parse run/github-script steps; build transfer and sink edges
  -> evaluate guards (identity-binding vs syntactic) and if-conditions
  -> evaluate CP-CICD-* controls (§2.1 mapping and aggregation)
  -> apply owners and exceptions (never removing findings)
  -> emit evidence graph + findings + prerequisites + coverage
  -> JSON / SARIF / Markdown
```

An unknown guard never silently becomes `true` or `false`. Only same-repository flows are analyzed in v0.1. Cross-repository artifact flows are UNKNOWN and carry explicit unsupported-edge metadata.

### 3.3 Proposed Go repository structure

```text
controlproof/
  cmd/controlproof/main.go
  internal/config/          (policy, repo_profile, owners, exceptions)
  internal/workspace/
  internal/workflow/        (actionlint adapter, inlining, normalization)
  internal/model/
  internal/semantics/permissions/
  internal/semantics/triggers/
  internal/shell/           (mvdan.cc/sh AST → commands, GITHUB_* writes)
  internal/summaries/       (versioned action summaries: download-artifact, dawidd6, github-script, checkout)
  internal/graph/
  internal/taint/
  internal/rules/cicd001/
  internal/rules/cicd002/
  internal/rules/cicd003/
  internal/evidence/
  internal/report/jsonv1/
  internal/report/sarif/
  internal/report/assurance/
  internal/fixture/
  schemas/controlproof-report-v1.json
  schemas/controlproof-policy-v1.json
  testdata/fixtures/{safe,vulnerable,unknown}/
  testdata/corpus/          (manifest of pinned public repos + triage ledger; repos not vendored)
  tests/golden/
  docs/{architecture,threat-model,control-catalog,benchmarks}/
  .github/workflows/
  LICENSE
  SECURITY.md
  CONTRIBUTING.md
```

Use the conventional tooling: `go test ./...`, gofmt, `go vet`, staticcheck, govulncheck and dependency updates. The only planned third-party dependencies are actionlint and mvdan.cc/sh. Pin both, and review them before every upgrade. Nothing is downloaded at runtime by default.

### 3.4 CLI contract

```bash
controlproof scan .
controlproof scan . --format json --output report.json
controlproof scan . --format sarif --output controlproof.sarif
controlproof scan . --policy .controlproof.yaml --fail-on high
controlproof scan . --strict-coverage
controlproof explain <finding-id> --report report.json
controlproof report --since 2026-07-01 reports/*.json --format html --output assurance.html
controlproof fixtures verify --profile isolated
controlproof version
```

**Exit status:**

| Code | Meaning |
|---|---|
| `0` | Completed; no FAIL at or above the configured fail level; no expired exceptions. |
| `1` | Completed; policy gate failed (FAIL at or above the level, or an expired exception). |
| `2` | Scan, parser, config or internal error. |
| `3` | `--strict-coverage` was requested, and some control is UNKNOWN or the share of opaque steps on candidate paths exceeds `coverage.max_opaque_ratio`. |

UNKNOWN results are always visible. They fail the gate only under `--strict-coverage` or `policy.fail_on_unknown: true`. Errors are never conflated with security violations.

**Defaults:** no network access; never execute repository code; local only. Metadata enrichment comes later as an explicit opt-in.

Example `.controlproof.yaml` (illustrative; versioned by `schemas/controlproof-policy-v1.json`):

```yaml
schema_version: 1
repo_profile:
  provider: github
  repository_default_token_permissions: unknown   # read | write | unknown
  fork_pr_workflow_approval: unknown              # all-outside-collaborators | first-time-contributors | none | unknown
controls:
  CP-CICD-001: {enabled: true, fail_on: medium}
  CP-CICD-002: {enabled: true, fail_on: medium}
  CP-CICD-003: {enabled: true, fail_on: high}
policy:
  fail_on_unknown: false
coverage:
  max_opaque_ratio: 0.2
owners:
  - fingerprint: cp_3f9a...
    owner: "@platform-team"
    target_date: 2026-11-30
exceptions:
  - fingerprint: cp_7c21...
    approver: "@security-lead"
    rationale: "Consumer runs only on internal repo; forks disabled at org level."
    compensating_controls: ["org policy: fork PR workflows disabled"]
    review_date: 2026-12-15
    expires: 2027-01-31
reporting:
  include_unknown: true
  redact_secrets: true
```

### 3.5 JSON evidence model

```json
{
  "schema_version": "1.0.0",
  "scan_id": "<uuidv7>",
  "repository": {"provider": "github", "path": "org/repo", "commit_sha": "<40-hex-sha-or-null>"},
  "scanner": {"name": "ControlProof", "version": "0.1.0"},
  "policy_sha256": "<hex>",
  "scan_mode": "offline-static",
  "started_at": "2026-10-30T10:00:00Z",
  "completed_at": "2026-10-30T10:00:02Z",
  "controls": [{
    "control_id": "CP-CICD-001",
    "status": "FAIL",
    "coverage": "modeled-path",
    "finding_ids": ["cp_54..."],
    "unknown_reasons": []
  }],
  "findings": [{
    "finding_id": "cp_54...",
    "rule_id": "CP-CICD-001",
    "severity": "high",
    "severity_justification": "Arbitrary JS executes via github-script with issues:write and pull-requests:write in default-branch context (cache-poisoning reach).",
    "confidence": "confirmed-static",
    "title": "Untrusted artifact can overwrite an executed helper",
    "producer": {"workflow": ".github/workflows/capture.yml", "trigger": "pull_request_review", "trust": "fork-definition-controlled"},
    "consumer": {"workflow": ".github/workflows/apply.yml", "job": "update", "privilege": {"token": {"issues": "write", "pull-requests": "write"}, "secrets": [], "persist_credentials": false, "runner": "github-hosted", "cache_write": "default-branch"}},
    "source": {"file": ".github/workflows/capture.yml", "line": 31},
    "sink": {"file": ".github/workflows/apply.yml", "line": 47},
    "path": [{"edge": "produces", "evidence": "..."}, {"edge": "extracts_to", "evidence": "..."}, {"edge": "imports", "evidence": "..."}],
    "prerequisites": ["fork PR workflows run without maintainer approval, or attacker is a prior contributor"],
    "limitations": [],
    "governance": {"owner": null, "exception": null},
    "remediation": "Download into ${{ runner.temp }}/review, parse review.json as data, and never require() from the extraction tree."
  }],
  "coverage": {"supported_workflows": 4, "inlined_local_actions": 1, "opaque_steps_on_candidate_paths": 1, "unknown_expressions": 2}
}
```

**Production schema requirements:**

- Scan IDs are UUIDv7.
- The finding fingerprint is derived from `(rule_id, producer workflow path, consumer workflow path + job id, sink kind + normalized sink target, normalized semantic path)`. Line numbers and timestamps are excluded.
- Renaming a workflow file or job produces a new fingerprint. The assurance report links the old and new findings heuristically and marks the link as `heuristic`.
- Record the canonical repository path and commit. Include an absolute file path only if explicitly configured.
- Redact token-like strings in snippets, and never include user credentials.
- SARIF:
  - `codeFlows`/`threadFlows` carry the producer→consumer→sink path
  - `relatedLocations` carry the second file
  - `partialFingerprints.controlproof/v1` carries the fingerprint

### 3.6 Deferred: server and dashboard (post-v0.1)

v0.1 has no server (ADR-005). If a central view is later justified, it must meet these requirements:

- Single-node Go service with SQLite via `modernc.org/sqlite` (pure Go, so release builds stay reproducible without cgo).
- Localhost-only by default, with **Host-header validation** against DNS rebinding and **Origin checks / CSRF tokens** on every state-changing request.
- OIDC authentication and TLS at a trusted reverse proxy before any remote exposure.
- Imported reports labeled `untrusted-import`, schema-validated and size-bounded.
- React rendering with no `dangerouslySetInnerHTML`, plus a strict CSP.
- Immutable scans, with triage stored separately. Exceptions never erase findings, and an audit log records every change.

## 4. Threat model — including threats to ControlProof

### 4.1 Protected assets

Source code, repository configuration, CI credentials and workflow secrets, signing/publishing authority, integrity of scan results, exception decisions and remediation evidence.

### 4.2 Trust boundaries

- External contributors / PR-controlled content (including PR-modified workflow definitions) ↔ repository trusted workflows.
- Unprivileged GitHub Actions runs ↔ privileged and default-branch-context runs (including the Actions cache).
- Downloaded artifact ↔ trusted source and shell environment.
- Untrusted repository contents (including `.controlproof.yaml` in a PR) ↔ ControlProof parser/analyzer.
- Stored reports ↔ assurance report rendering.
- Optional GitHub App token ↔ GitHub API.

### 4.3 Attack / abuse scenarios and mitigation

| Threat | Likely path | Mitigation | Test |
|---|---|---|---|
| Crafted YAML crashes scanner | Excessive aliases, nesting, huge strings | Bounded size, depth, alias expansion and time; fuzzing; panic recovery per file | Fuzz parser; 10 MB and alias-bomb payloads |
| Path traversal/symlink exfiltration | Repo script or local action reference escapes root | Canonical root containment; deny symlinks by default | `../../secret`, symlink fixtures |
| Scanner executes malicious repository code | Rule invokes shell/third-party action | No repo code execution in static mode; shell is parsed, never run | Instrument subprocess calls; verify none |
| Unsafe fixture runtime escape | Docker fixture has network/privileged mounts | Isolated unprivileged container, no secrets, no writable host mounts; no Docker socket | Isolation smoke tests |
| Report HTML/script injection | User-controlled finding title/snippets/rationale | HTML report escapes all text; no inline script from data; CSP meta | XSS fixtures in assurance report |
| Secret leakage in evidence | Source snippet contains token | Redact, minimize snippets, no raw logs by default | Seeded canary values in reports |
| Self-granted exception | PR author adds an exception for their own finding in `.controlproof.yaml` | Exceptions require `approver`; docs require CODEOWNERS on `.controlproof.yaml`; report shows exception author commit if available | Exception without approver fails validation |
| Forged report in assurance input | Attacker supplies a false PASS JSON | Report records `scan_id`, commit and checksum; assurance report labels inputs not produced in CI as `untrusted-input` | Tampered JSON fixture |
| Misleading PASS | Unknown library/action semantics omitted | Coverage gating; UNKNOWN by default | Unsupported-action fixtures |
| Privilege leakage from optional integrations | Excessive PAT/GitHub App scope | Read-only GitHub App; no actions write; token never persisted | Permissions review |
| Exception abuse | Findings hidden forever | Expiry required; expired exception fails gate; git history is the audit log | Exception-expiry test |

### 4.4 Active test safety contract

Active tests operate only on **synthetic repository fixtures**, either generated by ControlProof or deliberately supplied by an operator. The default `scan` never runs active tests. Running them requires all of the following:

- a separate command, with visible confirmation or the explicit `--allow-fixture-execution` flag
- no internet and no secrets
- CPU, memory and pid limits
- a temporary filesystem
- an unprivileged user
- an isolated network
- a bounded duration

Never attach `/var/run/docker.sock`, a writable host workspace mount or an SSH agent. If the host lacks safe isolation, the runner returns `UNAVAILABLE` rather than falling back to an unsafe mode.

## 5. Quality, reliability and privacy requirements

| Category | Requirement |
|---|---|
| Determinism | The same repo snapshot, policy and scanner version yield equivalent semantic results and IDs; all map iteration is sorted before output. |
| Failure safety | Parse errors and unresolved actions are visible; never report PASS from partial parsing. |
| Privacy | Offline scanner by default, no cloud calls, no LLM submission of code, sanitized evidence. |
| Performance | Target p95 <5s for 100 standard-sized workflow files on a defined local benchmark machine. |
| Scalability | Repo-level scans; no claim of organization-scale throughput. |
| Portability | Linux/macOS first, Windows via CI when feasible; pure-Go, reproducible release builds. |
| Output stability | Versioned schema, SARIF 2.1.0, stable rule IDs; deprecate changes explicitly. |
| Secure configuration | No implicit elevated token or active test; external integrations opt-in. |
| Supportability | Clear error codes, debug log redaction, version and policy hash in every report. |
| Accessibility | The HTML report uses semantic markup, text status labels (never color-only) and WCAG AA contrast. |

Release checks: `go test` (with race where relevant), `go vet`, staticcheck, govulncheck, the fixture suite, SARIF validation, reproducible builds, and least-privilege GitHub Actions with pinned dependencies.

## 6. Threat detection implementation notes

### 6.1 Trigger/permission semantics

- **`workflow_run` risk.** A `workflow_run` workflow is higher risk but not automatically vulnerable. It can receive write tokens and secrets even when the producer could not, and it always runs in the default-branch context.
- **Permission precedence.** Explicit job permissions override workflow permissions. Listing any permission sets every unspecified one to none. With no `permissions` key, the token takes the repository or organization default, which is `unknown-default` unless `repo_profile` resolves it.
- **OIDC.** Do not infer publish authority from `id-token: write` unless the federation trust policy is known; record it as a prerequisite.
- **Explicitly restricted jobs.** A `workflow_run` job with explicit restrictive permissions and no secrets is not high-privilege. Code execution in it still meets the §2.3 impact floor because of cache poisoning.
- **Persisted credentials.** If `actions/checkout` runs without `persist-credentials: false`, any code that executes later in the job can read the token.

### 6.2 Artifact matching and producer identity

**Upload side:** capture `name`, `path`, step/job, origin context and conditions.

**Download side:** capture `name`, `pattern`, `merge-multiple`, `path`, `run-id`, `repository` and `github-token` for `actions/download-artifact`. For `dawidd6/action-download-artifact`, capture `run_id`, `workflow`, `branch`, `allow_forks`, `search_artifacts` and `path`.

**Producer identity rules:**

- `workflow_run.workflows` matches by workflow *name*. A fork PR can add a workflow with the same name, or edit the existing producer. **A name match only establishes candidates; it never proves identity.** The consumer binds identity only through `run-id` from the event *and* checks on `github.event.workflow_run.head_repository` or the event's head SHA.
- If any candidate producer is fork-runnable (§2.3), its contents are **arbitrary**. Do not use the producer's declared upload `path` to bound what the artifact contains; the attacker can rewrite that step. This is a high-confidence claim, not a weak one.
- If every candidate producer has a trusted definition but builds untrusted code (`pull_request_target` or `issue_comment` with a PR-head checkout), the contents are attacker-influenced only within the producer's upload `path`. Analyze that `path` for overlap with the consumer's executed files. If the overlap can't be determined statically, report `potential` confidence.

**Extraction:** model the default extraction path (the workspace) and normalize paths. If dynamic expressions affect the path, emit UNKNOWN. In tests, use synthetic archives with explicit file lists.

### 6.3 Dataflow and sink catalog

**Execution sinks (CP-CICD-001)**, for a file under the extraction tree:

- `uses: ./path` local actions
- `run:` invoking `./x`, `bash x`, `sh x`, `source x` / `. x`, `python x`, `node x`
- `actions/github-script` `require('./...')` or `import`
- `npm ci|install|test|run`, `yarn`, `pnpm` when `package.json` is in the tree
- `pip install .` / `-e .` with a project file in the tree
- `make` with a `Makefile` in the tree
- `go run|generate|test` when `go.mod` is in the tree

**Transfers (CP-CICD-002):**

- `cat`/`jq`/`$(< file)` assigned to a variable
- `echo "k=..." >> "$GITHUB_OUTPUT"` → `steps.<id>.outputs.k`
- `>> "$GITHUB_ENV"` → `env.K` in later steps
- `fs.readFileSync` / `JSON.parse` in github-script → `core.setOutput`

**Destination sinks:**

- `git push <remote> <refspec>`
- `actions/checkout` `ref:` / `repository:`
- `gh api` mutation paths
- `gh workflow run`, `repository_dispatch`
- `npm publish`, `twine upload`
- writes to `$GITHUB_ENV` / `$GITHUB_PATH`

**Opacity and summaries:**

- Never interpret shell; parse it only. Command substitution, loops, functions and `eval` on a candidate path produce `opaque_step` and UNKNOWN.
- An unknown script or action on the candidate path is recorded as `opaque_read_or_execution`, and the control contribution is UNKNOWN.
- Third-party action summaries live in `internal/summaries/`. Each records provenance, the action version range it was written against, and a fixture.
- Avoid an LLM as the enforcement engine. If one is later used for explanations, label its output as unverified commentary.

### 6.4 Severity and confidence

Severity depends on demonstrated sink impact (write token, publish authority, secrets, cache-poisoning reach) and on the attacker-control prerequisite. It is kept separate from confidence that the path exists. Every finding stores `severity_justification`.

`Potential` confidence produces UNKNOWN, never a fabricated confirmed exploit. `Conditional` confidence produces a FAIL with listed prerequisites (§2.1). Severity can be raised per asset criticality in policy, with a stored justification.

## 7. Test plan, fixtures and validation

### 7.1 Fixtures and corpus

Each fixture has **exactly one expected verdict** per control, plus an expected confidence.

| Fixture | Pattern | Expected |
|---|---|---|
| A — SPIRE pattern | `pull_request_review` producer; `workflow_run` consumer sparse-checks out scripts, downloads into workspace, github-script `require`s overwritten helper | CP-001 FAIL `confirmed-static` |
| A′ — SPIRE fixed | Download into `${{ runner.temp }}/review`; only `review.json` parsed | CP-001 PASS |
| B — attest pattern | Fork-rewritable `pull_request` producer writes `head-ref`; `workflow_run` consumer with `contents: write` validates charset only, checks out and pushes to that ref | CP-002 FAIL `confirmed-static`, guard `syntactic-only` |
| B′ — attest fixed | Destination derived from `github.event.workflow_run.head_branch` after checking `head_repository.full_name == github.repository` | CP-002 PASS |
| C — safe data-only | Extract to temp dir, schema-validate, use only for a constrained comment | CP-001 PASS |
| D — opaque action | Third-party downloader with no summary on candidate path | CP-001 UNKNOWN |
| E — no boundary | No `workflow_run` consumer of any artifact | All controls NOT_APPLICABLE |
| F — restricted consumer | Same as A but `permissions: {}`, no secrets, `persist-credentials: false` | CP-001 FAIL `confirmed-static`, severity `medium` (cache-poisoning impact floor) |
| G — unbound selector | `dawidd6/action-download-artifact` without `run_id`; artifact reaches `git push` | CP-003 FAIL `conditional` |
| G′ — bound selector | `run-id: ${{ github.event.workflow_run.id }}` plus `head_repository` check | CP-003 PASS |
| H — default permissions | Same as A with no `permissions` key and secrets absent | CP-001 FAIL `conditional`, prerequisite `repository_default_token_permissions` |
| I — `$GITHUB_ENV` sink | Artifact value written to `$GITHUB_ENV` in privileged job | CP-002 FAIL `confirmed-static` |
| J — inlined composite | Download and execution happen inside a local composite action | CP-001 FAIL `confirmed-static` |
| K — approval-to-SHA mismatch (P1) | Authorized revision A, mutable checkout B | CP-004 FAIL (only if CP-004 is implemented) |

**Public corpus.** Pin a manifest of public repositories (by commit SHA) that contain `workflow_run` consumers that download artifacts. Clone them once, then scan offline.

- Manually triage every FAIL into `testdata/corpus/ledger.md` as TP, FP or uncertain, with a reason.
- Report precision, UNKNOWN% and the top opaque-step causes.
- Disclose any true positive privately through the maintainer's security policy or GitHub private vulnerability reporting, never in a public issue. Publish details only after a fix or the agreed disclosure deadline.

### 7.2 Testing layers

| Layer | Test | Exit condition |
|---|---|---|
| Parser | Anchors/aliases, multiline, `on`, reusable workflows, composite actions, malformed input | No panic; accurate locations through inlining |
| Semantics | Triggers, `permissions`, fork contexts, job overrides, privilege record | Golden IR snapshots |
| Shell | Output/env propagation, recognized commands, opaque constructs | Golden transfer/sink edges |
| Graph | Produce/download/reachability/guard resolution | Deterministic edges and unknowns |
| Rules | Fixture table §7.1 | Every fixture matches its single expected verdict |
| Reporting | JSON schema/SARIF validator, redaction, fingerprints, HTML escaping | Zero invalid reports |
| Security | Path traversal, symlink escapes, malicious snippets, fixture isolation | No host access or injection |
| Corpus | Public corpus, triage ledger | Precision and UNKNOWN% reported each release |
| Competitive | CodeQL `actions-security-extended`, zizmor, poutine, octoscan on fixtures and corpus | Every incremental result and false positive documented |

**Research performed:**

- Read vendor documentation, both GHSL advisories, CodeQL's Actions query list, the zizmor audit list and the octoscan rule list.

**Not performed:**

- implemented source analysis
- exploit reproduction
- benchmark scans
- integration tests
- user interviews
- buying tests

All numerical targets are proposed engineering thresholds.

### 7.3 False-positive handling

A security reviewer should be able to see `source`, `transfer`, `sink`, `prerequisites`, and `why the guard is inadequate`. If a path cannot be demonstrated, mark it UNKNOWN and request targeted manual validation. Every confirmed false positive becomes a regression fixture plus a ledger entry. Never auto-file a public GitHub issue that exposes a newly discovered vulnerability.

## 8. Product governance and risk model

### 8.1 Governance entities

- **Asset:** a repository, and optionally a critical release workflow, with business criticality (`low`, `moderate`, `high`, `critical`), an owner and a product. Declared in `.controlproof.yaml`.
- **Control:** CP-CICD-xxx with a clear objective, tested scope, threat, assumptions and latest evaluation.
- **Finding:** a reproducible unsafe path with evidence, impact, prerequisites and confidence.
- **Ownership:** an owner and target date per fingerprint, declared in `.controlproof.yaml`.
- **Exception:** a separate risk acceptance with an accountable approver, rationale, compensating controls, review date and expiry. Declared in `.controlproof.yaml`. The approving PR is the decision record.
- **Retest:** a later scan with the same fingerprint absent and the same control evaluated (not UNKNOWN). This confirms the original unsafe path is no longer found within supported coverage.

### 8.2 Metrics for leadership

**Do report:**

- critical repos in scope and critical release paths evaluated
- FAIL/UNKNOWN/PASS counts
- percent of applicable controls with sufficient evidence
- time-to-fix (first seen → verified retest)
- overdue ownership and exception age
- detection coverage against fixtures
- recurrence of failed controls after remediation

**Do not report:** "X% secure", "100% supply-chain protected", fabricated prevented-breach counts, or arbitrary financial ROI.

**Risk priority:** combine business asset criticality, evidenced privilege and sink impact, control confidence and exposure prerequisites. Keep the values visible and auditable. Avoid proprietary scoring until validated by users.

**Optional standards mapping:** align individual checks with the OWASP Top 10 CI/CD Security Risks and relevant NIST SSDF / SLSA practices, only after verifying the exact clauses. A mapping is not certification or proof of compliance.

### 8.3 Assurance report and third-party findings

`controlproof report` reads N ControlProof JSON reports, plus optionally CodeQL and zizmor SARIF, and the current `.controlproof.yaml`. It produces a static HTML or Markdown report covering:

- control status per asset over time
- open findings with owners and target dates
- verified retests
- exceptions with age and expiry
- coverage and unknowns
- per-input provenance (scan ID, commit, checksum, `untrusted-input` label)

Third-party SARIF findings are fingerprinted by `(tool, ruleId, partialFingerprints or location hash)` and take part in the owner, exception and retest lifecycle. They do not contribute to CP-CICD control status.

## 9. Solo full-time roadmap (12 weeks)

| Weeks | Work | Deliverable | Gate |
|---|---|---|---|
| 0 (3 days) | Fixtures A, A′, B, B′, C, F, G as plain repos; run CodeQL/zizmor/poutine/octoscan; start user interviews | Competitive baseline table | §1.6 week-0 decision recorded as ADR |
| 1 | Repo skeleton, actionlint adapter, anchor resolution, local action/reusable workflow inlining | `scan` emits typed IR | Parser golden cases pass, locations survive inlining |
| 2 | Triggers incl. review events, privilege record, `repo_profile` | Privilege contexts | Explicit `permissions: {}` vs `unknown-default` distinguished; persist-credentials and secrets captured |
| 3 | Artifact graph, producer identity rules (§6.2), download summaries | Graph JSON debug dump | A and G link correctly; fork producers marked arbitrary-content |
| 4 | Sink catalog; CP-CICD-001 | First rule | A, A′, C, D, F, H, J match expected verdicts |
| 5 | Shell AST, `$GITHUB_OUTPUT`/`$GITHUB_ENV` propagation, github-script recognition | Transfer edges | Golden shell tests pass |
| 6 | CP-CICD-002 with guard adequacy; CP-CICD-003 | Three-rule engine | B, B′, G, G′, I match; no unsafe PASS on unknown fixtures |
| 7 | JSON/SARIF (codeFlows, partialFingerprints), exit codes, GitHub Action packaging | Release candidate CLI | Offline, deterministic, valid SARIF that renders the path in GitHub code scanning |
| 8–9 | Public corpus scan, triage ledger, competitive benchmark, private disclosure of any TPs | Public benchmark incl. negative results | Keep/kill on incremental signal (§1.6) |
| 10 | Owners and exceptions-as-code, expiry gate, policy schema | Governance in CLI | Expired exception fails gate; exception never hides finding |
| 11 | `controlproof report` multi-scan assurance report, retest linking, third-party SARIF ingest | Leadership report | Demo: finding → owner → fix → verified retest → report |
| 12 | Documentation, threat model review, OSS packaging, pilot onboarding | Public beta `v0.1` | Pilot status reported honestly |

User interviews (§11.3) run in parallel from week 0. Pilots install the CLI as soon as the week-7 release candidate exists.

**Contingency:** if the analysis engine needs more time, cut weeks 10–11 to owners and exceptions only, and keep the corpus benchmark. Never sacrifice correctness or coverage signaling to meet a date. The timeline is a planning sequence, not a guarantee.

## 10. LLM-assisted engineering workflow

LLMs can help generate parsing test fixtures, code scaffolding, threat-model drafts, documentation and benchmark hypotheses, but **they are not the security oracle**.

Required practices:

1. Keep repository content local by default. Do not send third-party private workflow YAML or credentials to a model without explicit authorization. Public corpus repositories are public data, but triage conclusions about undisclosed vulnerabilities are not.
2. Every detector rule requires a written invariant, at least one FAIL fixture, one PASS fixture and one UNKNOWN fixture.
3. Treat untrusted repository content as *data*, never as instructions to the AI agent. Guard against prompt injection when reviewing code from unknown contributors or corpus repos.
4. Validate generated code with unit tests, race and fuzz tests where applicable, linting, library documentation, and peer review when possible.
5. Maintain a design decision record and a **false-positive ledger** with regression fixtures.
6. Restrict AI agent tool privileges: no production credentials, no automatic issue disclosures, no `git push` to protected branches, no customer data access.
7. Require human approval for any new dependency, network access, external telemetry or CI permission change.
8. Document known limitations honestly. Never let model-written explanations override rule-engine evidence.

## 11. Distribution, OSS strategy and market validation

### 11.1 Launch positioning

"**ControlProof evidences specific CI trust-boundary failures across workflows, with explicit prerequisites, honest unknowns and an accountable remediation trail.**"

Start with a developer-focused CLI. GitHub code scanning's SARIF upload availability depends on repository visibility and plan, so JSON, Markdown and HTML output must work everywhere.

### 11.2 OSS minimum package

- Apache-2.0 LICENSE, README quickstart, `SECURITY.md`, `CONTRIBUTING.md`, architecture, model scope, rule documentation and sample outputs.
- Reproducible cross-platform binaries with checksums and, if feasible, provenance attestations.
- Synthetic vulnerable and safe fixtures, the corpus manifest, the triage ledger (without undisclosed details) and the benchmark methodology.
- A GitHub Action with minimal permissions (`contents: read`) and no automatic reporting that exposes private findings.
- Responsible-disclosure documentation and a private security report channel.

### 11.3 User discovery and commercial falsification

Starting in week 0, talk to 8–12 security-minded engineers or CTOs at SMBs with nontrivial GitHub Actions deployments. Ask for:

- sanitized, workflow-specific examples
- prior incidents and near-misses
- current tools
- who owns remediation

**Behavior over sentiment:** ask participants to run the tool on their own repo, review every finding, and then keep or remove it. Measure review minutes, false alarms, fixed findings and recurring CI use.

**Go/no-go after pilots,** measured 30 days after the week-7 release candidate. These are hypothesized thresholds, not forecasts:

- at least 3 of 5 teams keep the tool in CI
- at least 2 report independently verified findings beyond incumbent output
- at least 2 express procurement-backed willingness to pay for governance features

If these are not met, keep the project's OSS and portfolio value and do not build a SaaS. Commercial failure does not fail the primary goal.

### 11.4 Competitive positioning

| Existing tool | Existing coverage relevant here | What ControlProof must prove additionally |
|---|---|---|
| CodeQL (Actions) | `artifact-poisoning/{critical,medium}` path queries; `envvar-injection`, `envpath-injection`, `untrusted-checkout-toctou`, `cache-poisoning-*`, `code-injection` | Identity-binding guard analysis (attest class); explicit prerequisites and `conditional` vs `confirmed` confidence; github-script `require` sinks; offline use without CodeQL setup; governance lifecycle |
| zizmor | `dangerous-triggers`, `cache-poisoning`, `github-env`, `template-injection`, `excessive-permissions`; no artifact audit | Cross-workflow artifact paths with source-to-sink evidence |
| octoscan | `dangerous-action` (untrusted-artifact actions), `dangerous-write` (controlled `GITHUB_OUTPUT`/`GITHUB_ENV`), `dangerous-checkout` | Cross-workflow path proof and guard adequacy rather than per-step warnings |
| poutine | Multi-CI pipeline risk rules | GitHub-specific artifact trust-boundary depth |
| OpenSSF Scorecard | Dangerous-workflow and token-permission posture | Artifact boundary semantics and ownership evidence |
| GitHub attestation verification | Signed artifact provenance | Whether the consumer verifies provenance and isolates content; a signed artifact can still be untrusted for a given sink |
| Manual security review | Maximum contextual judgment | Reproducibility, repeat scans, drift detection and audit trail |

## 12. Final definition of done (v0.1)

The project is ready to present as a credible Head of Cybersecurity portfolio artifact when:

1. A public `controlproof scan .` CLI reliably identifies both incident-inspired cases (fixtures A and B) and distinguishes their remediated counterparts, and every §7.1 fixture matches its expected verdict.
2. Evidence includes source, path, sink, privilege, guard adequacy, attacker prerequisites, confidence and clear limits. No security conclusion is manufactured from unknowns.
3. The scanner has a published threat model, secure execution defaults, parser fuzzing and documented data-handling protections.
4. SARIF and JSON reports are valid and deterministic, with versioned schemas, and SARIF paths render in GitHub code scanning.
5. Exceptions-as-code and the assurance report show critical assets, control evaluations, owners, exceptions and verified retests without conflating coverage with security posture.
6. A published benchmark compares CodeQL, zizmor, poutine and octoscan against ControlProof on the fixtures and the public corpus, including failed hypotheses and false positives.
7. A demonstration shows: vulnerability → detection → assigned owner → remediation → verified retest → leadership report.
8. Pilot feedback and coverage limits are documented honestly. The result is worthwhile as OSS even if no commercial differentiation is found.

## 13. Key primary sources and technical references

1. GitHub Security Lab, 2026-09-16, SPIFFE/SPIRE artifact poisoning: https://securitylab.github.com/advisories/GHSL-2026-204_spiffe_spire_repository/
2. GitHub Security Lab, 2026-09-16, actions/attest artifact metadata injection: https://securitylab.github.com/advisories/GHSL-2026-225_actions_attest/
3. GitHub Actions `workflow_run` event semantics: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run
4. GitHub Actions workflow permissions reference: https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax
5. GitHub Actions secure use reference: https://docs.github.com/en/actions/reference/security/secure-use
6. OWASP GitHub Actions Security Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/GitHub_Actions_Security_Cheat_Sheet.html
7. CodeQL query help for GitHub Actions: https://codeql.github.com/codeql-query-help/actions
8. CodeQL artifact poisoning (medium): https://codeql.github.com/codeql-query-help/actions/actions-artifact-poisoning-medium
9. CodeQL PR excluding `runner.temp` extraction from artifact poisoning: https://github.com/github/codeql/pull/19388
10. zizmor audits: https://docs.zizmor.sh/audits/
11. octoscan: https://github.com/synacktiv/octoscan
12. OpenSSF Scorecard checks: https://github.com/ossf/scorecard
13. actionlint Go API: https://pkg.go.dev/github.com/rhysd/actionlint
14. dawidd6/action-download-artifact inputs: https://github.com/dawidd6/action-download-artifact
15. GitHub artifact attestations (and requirement to verify): https://docs.github.com/en/actions/concepts/security/artifact-attestations
16. GitHub CLI verification semantics: https://cli.github.com/manual/gh_attestation_verify
17. GitHub artifacts REST API and read permissions: https://docs.github.com/en/rest/actions/artifacts?apiVersion=2022-11-28
18. GitHub SARIF upload availability: https://docs.github.com/en/code-security/how-tos/find-and-fix-code-vulnerabilities/integrate-with-existing-tools/upload-sarif-file

---

**Specification confidence:** The security failure class is supported by original public advisories and official GitHub documentation. Existing tool coverage was checked against vendor documentation on 2026-10-30. That check does not show how well each tool detects these patterns in practice; the week-0 benchmark measures that. The proposed analyzer, roadmap targets, implementation costs and commercial utility are design hypotheses. No code has been built and no customer pilot has been run as part of this specification.
