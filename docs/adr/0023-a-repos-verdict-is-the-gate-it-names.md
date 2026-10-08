# A repository's verdict is the gate it names

## Status

Accepted (2026-10-08)

## Context

The products lens gives every repo in a product one CI cell. It was computed
over every open pull request in the repo at once: a failing check anywhere made
the whole repo `✗ failing`, else anything running made it `● deploying`, else
anything passing made it `✓ green`, else `—`.

Reported as a bug: `player-app` read `✗ failing` while its trunk was green and
a pull request was sitting ready to merge. The cell was not wrong about its
inputs. It was measuring the wrong thing.

That repository has an umbrella workflow, `.github/workflows/pre-merge.yml`,
with an aggregator job named `All gates green`. The workflow says why in its
own header:

> `merge_group` gates on a single workflow's overall result. We can't make
> sibling workflows mutually required for it. The umbrella's `all-green`
> aggregator job becomes the sole required check, which collapses the
> branch-protection UI from "N rows" to "1 row".

and again on the job itself:

> Mac pool, PERMANENTLY (both profiles): the aggregator is the ONLY required
> check […]

The job `needs` every lane, runs `if: always()` so it reports red rather than
skipped when an upstream job fails, and is profile-aware: a lane skipped because
the active profile deliberately turned it off is allow-listed, while a lane
skipped for any other reason fails the aggregator.

Once a repository has that, the individual check conclusions beside it are
noise, in both directions. `SKIPPED` is normally a design decision — no Android
emulation on the Mac pool while the Nix box is free — and a `FAILURE` in one
lane is usually already reflected in the aggregator's own verdict. The old rule
read both as "this repository is broken".

Measured on `Player-Pulse/player-app`, 2026-10-08, from one
`gh pr list --state open --json number,statusCheckRollup`:

| PR | rollup entries | `All gates green` |
| --- | --- | --- |
| 670 | 32 | FAILURE |
| 669 | 11 | *absent* |
| 668 | 9 | *absent* |
| 665 | 44 | SUCCESS |
| 654 | 9 | *absent* |
| 246 | 32 | FAILURE |
| 191 | 4 | *absent* |

Seven open pull requests: one green by the gate and unmerged, two red by the
gate, and four carrying no gate result at all, because the umbrella skips its
aggregator on targeted and dispatch runs (`ios_only` / `android_only` /
`web_only`) where some of its dependencies would legitimately be skipped. PR
670's rollup, the one that made the repo red, also contains lanes concluding
`CANCELLED` and `SKIPPED`, which say nothing about whether anything can ship.

**GitHub cannot be asked which check is the required one.** Both routes fail
here, and they fail differently:

- the branch-rules API is not on this plan —
  `gh api repos/Player-Pulse/player-app/rules/branches/main` returns
  `HTTP 403`, "Upgrade to GitHub Pro or make this repository public to enable
  this feature";
- and GitHub's own merge verdict disagrees with the gate anyway —
  `gh pr view 670 --json mergeStateStatus,mergeable` reports
  `UNSTABLE` / `MERGEABLE`, i.e. mergeable, while that pull request's
  `All gates green` is `FAILURE`.

So the cockpit cannot discover the aggregator. It must be told, or say nothing.

## Decision

**A repository's verdict is the one check it names, and naming one is optional.**

`[gates]` in `config.toml` maps a repo directory name to that check:

```toml
[gates]
player-app = "All gates green"
```

A repo with an entry gets a cell that answers *what does this repository want
from me*:

- open pull requests whose gate is `SUCCESS` lead the cell — `1 ready` — in
  **amber**, the role this cockpit already uses for "a human is wanted". A gate
  that has gone green on a pull request still open is asking to be merged, which
  is the one fact in this cell that is a claim about the human (ADR 0003).
- pull requests whose gate is `FAILURE` follow it dim and only above zero:
  `1 ready · 2 red`. A red gate is the dispatcher's business, not a thing for
  the human to do, so it reads a shade below the figure it follows.
- with nothing ready, the red count leads on its own in the red role (`2 red`).
  This case is not in the original brief. Appending ` · 2 red` to a `0 ready`
  would be amber claiming a human is wanted for nothing, which the palette's
  roles forbid.
- a pull request with **no gate result** is counted in nothing and rendered
  nowhere. No verdict is not a verdict. A gate still running (`PENDING`) is not
  one either, nor is one that concluded `CANCELLED` or `SKIPPED`.
- no gate result anywhere, or a forge read that failed, renders `—`, which in
  this cockpit means "we could not see" and never "nothing is wrong".

A repo with **no** `[gates]` entry renders exactly the cell it rendered before,
byte for byte. The old rule is now `prodCICell`, a function of its own rather
than a branch inside the new one, so a repo that opts in cannot change what a
repo that did not says.

**The gate costs no API call.** `gh.RepoPRs` already fetches every open pull
request's `statusCheckRollup` in one `pr list` per repository (ADR: the forge
bill counts repositories, not pull requests), and the gate's conclusion is
inside that payload. The rollup rides along on `PRDetail` unexported, and
`PRDetail.GateState(name)` is the only way out of it — a caller may have the
verdict of a check it can name and not the list of check names, because handing
over the list invites a second opinion about what green means.

The gate's name is deliberately **not** a parameter of `RepoPRs`. That read is
memoised per repository and has to stay one entry: a name in the cache key would
buy the same payload again for every caller that spelled the gate differently,
and `PRChecksFor` / `PRReviewFor` name no gate at all. That is the bill this
package exists to stop.

A name can appear twice in one rollup — a re-run leaves the earlier job's entry
beside the new one, and these payloads carry four and five repeated names each
for other lanes — so `GateState` is deterministic and cautious: a settled
verdict beats an unsettled one, and among settled ones a failure beats a
success. Reporting green while a run of the same gate is red is the one answer
worth ruling out.

## Consequences

- Today, by the gate, `player-app` reads `1 ready · 2 red` where it read
  `✗ failing`. The four pull requests with no gate result are in neither figure.
- Every other repository in every portfolio is unaffected until its own name
  appears under `[gates]`. Nothing migrates, and an empty `[gates]` table in a
  freshly written config documents the key without turning anything on.
- A repo that names a gate loses the running/`● deploying` state in that cell.
  That is the trade: an aggregator with `if: always()` is pending for most of
  the umbrella's run, and the cell now reports verdicts rather than progress.
  Progress on a single pull request is still in the review queue's checks
  column, which counts lanes because a pull request is the unit there.
- Naming a check that does not exist is the same as naming none: every pull
  request reports absent, and the cell reads `—`. There is no validation pass
  and deliberately so — a check name is a fact about the repository's workflows,
  which change without telling us, and refusing a config on a stale spelling
  would take the whole products lens down with it.
- A second colour inside one fixed-width cell needed `crTail`, which splits a
  column into a lead and a quieter clause that together occupy the width the
  single cell had. A `seg` carries one foreground, so without it the clause
  would have had to take width from the repo name beside it.
- If GitHub ever makes the required-checks rule readable on this plan, the
  entry becomes a default rather than a requirement — but `mergeStateStatus`
  showing `MERGEABLE` over a FAILURE gate is a reminder that the forge's
  summary verdict is not the repository's, and a human-named gate stays the
  override.
