<!-- Summary: what changed and why. -->


## Release

Apply **one** label so the merge cuts the right version — the Release workflow reads it:

- `release:minor` — new feature (bumps `x.Y.0`)
- `release:major` — breaking change (bumps `X.0.0`)
- _no label_ — patch (bug fix / docs)
- `skip-release` — don't cut a release

(`release: minor` / `[skip release]` in the merge commit still work as a fallback.)
