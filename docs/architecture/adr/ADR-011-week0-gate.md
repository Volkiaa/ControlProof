# ADR-011: Week-0 competitive gate: CP-CICD-002 is the headline, CP-CICD-001 is a parity rule

- **Status:** Accepted
- **Date:** 2026-10-30
- **Evidence:** [Week-0 baseline](../../benchmarks/week0.md), [raw SARIF](../../benchmarks/week0/raw/), [fixtures](../../../testdata/fixtures/README.md)
- **Spec:** §0, §1.6, §11.4 of [controlproof-v0.2.md](../../spec/controlproof-v0.2.md)

## Context

Spec §1.6 requires running CodeQL (`actions-security-extended`), zizmor, poutine and octoscan against fixtures A, A′, B, B′, C, F and G before any engine work. The result decides the v0.1 focus through three outcomes:

1. CodeQL detects A and B with a usable trace and is silent on the fixed variants. CP-CICD-001 becomes a parity rule, and effort moves to identity-binding guard analysis, prerequisites and governance.
2. No tool detects B's guard inadequacy. CP-CICD-002 is the headline differentiator.
3. Every tool covers every fixture adequately. Engine work stops.

The baseline adds two probe variants of B, because a single B fixture can't show whether an alert depends on the guard:

- *B-push-only* keeps the artifact-chosen push destination but drops the untrusted checkout.
- *B-allowlist* keeps B but replaces the charset check with an adequate exact allowlist.

## Evidence

| Question | Result |
|---|---|
| Does CodeQL detect A with a usable trace? | Yes. `artifact-poisoning/critical` (9.0), with a consumer-only codeFlow from download to github-script. |
| Is it silent on A′ and C? | Yes. |
| Does it detect F at the right severity? | Yes. `artifact-poisoning/medium` (5.0). |
| Does CodeQL detect B? | Only incidentally. Its `untrusted-checkout/high` alert treats the checkout as code execution. It has no codeFlow and ignores both the push destination and the guard. |
| Does any tool's B alert depend on the guard? | No. CodeQL and octoscan alert identically on B and B-allowlist (false positive). |
| Does any tool see the destination sink? | No. CodeQL is silent on B-push-only (false negative). octoscan flags only a generic `$GITHUB_OUTPUT` write. |
| Does any tool detect G's unbound selector? | No. octoscan and poutine flag `dawidd6/action-download-artifact` by name only. |
| Does any trace span the producer and consumer files? | No, in all four tools. |

## Decision

1. **Outcome 2 applies to CP-CICD-002, which is the v0.1 headline.** The differentiator is identity-binding guard analysis plus artifact-controlled destination sinks. These are the two halves of B that no tool covers.
2. **Outcome 1 applies to CP-CICD-001, which becomes a parity rule.** It stays in v0.1 because §12 requires every fixture to match. It shares the graph and sink work that CP-CICD-002 and CP-CICD-003 need, and it is the first end-to-end slice in week 4. Its scope is capped at the §6.3 catalog. It must not grow sink coverage beyond CodeQL's just to compete. Its differentiation is limited to the producer-to-consumer trace, `prerequisites` and `confidence`.
3. **CP-CICD-003 is the second differentiator.** G has no meaningful coverage. G′ must be built so that silence on a bound selector is measured, not assumed.
4. **Cross-file evidence is a release requirement, not polish.** No incumbent's trace names the producer workflow, so SARIF `codeFlows` that span both files (#25) are part of the differentiation.
5. **Third-party SARIF ingest is raised to P0** (#35). CodeQL covers CP-CICD-001, so pilots will run both tools. The governance layer must ingest CodeQL and zizmor SARIF rather than duplicate them (§1.6 outcome 1).
6. **Engine work continues** (outcome 3 is not met). The roadmap order is unchanged.

## Consequences

- #22 (CP-CICD-002) acceptance gains B-push-only FAIL and B-allowlist PASS, and the finding must say why the guard is `syntactic-only`.
- #23 (CP-CICD-003) acceptance gains building G′ and staying silent on it.
- #17 (CP-CICD-001) is scoped to parity with the §6.3 catalog. A, A′, C, F, H and J still must match.
- #25 must render the producer → consumer path across both files.
- #35 moves from P1 to P0.
- #32 (week-8 benchmark) reuses `docs/benchmarks/week0/run.sh` and the probe fixtures as its fixture half.
- CP-CICD-004 (#42) is unaffected. CodeQL's `untrusted-checkout-toctou` overlap was not measured here, so it stays unscheduled P1.

## Revisit when

- A CodeQL actions-queries release adds destination or guard-aware artifact queries. Rerun `docs/benchmarks/week0/run.sh` with the new pack.
- The week-8 public-corpus benchmark (#32) shows no incremental true positive for CP-CICD-002 or CP-CICD-003. That triggers the §1.6 kill criterion.
