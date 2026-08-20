# A Linear token is the scope, and a shared one names no product

## Status

Accepted (2026-08-20)

## Context

The Linear backlog source was one `linear_api_key`: everything that one
token could see, merged into the backlog as an undifferentiated list,
tagged with no product at all. That is fine for a single workspace and
wrong for a portfolio. A Linear token is issued by a workspace and sees
only that workspace, so two products living in two workspaces could only
ever have one of them represented — the second product's tickets were not
missing, they were unreachable, and nothing on the screen said so.

The obvious shape is a token per product. That raises three questions the
one-key design never had to answer.

**What separates two products inside one workspace?** A `teams` filter in
our config is the tempting answer and the wrong one. The query asks for
`assignedIssues(first: 50)`; a filter we applied would narrow a list Linear
had already truncated for us, so a small team's work could be crowded off
the page by a busier one and read as a product with nothing open. Linear
grants an API key its teams at creation, which is a split the API enforces
before the fifty are chosen.

**What does a token two products share say about them?** Nothing — but a
map keyed by product invites the code to answer anyway, because the read
that survives deduplication has to be filed under something.

**What happens to the unscoped key once some product names a token?** The
same question in reverse: the ambient key is as likely to be another
workspace as one of the scoped ones, and the answer decides whether
adopting this feature is additive or destructive.

## Decision

**The token is the whole of the scope.** `[linear]` maps a product name to
the token its backlog is read with; `internal/linear.Assigned(key)` takes
the key as an argument and narrows nothing further. Where a workspace holds
two products, the human mints a team-scoped key each and pastes them both —
the config template, the README and the settings hint all say to scope the
key where it is created.

**A token two products share names neither.** `linearReads` resolves every
product's token before it names a single read, because how many products
claim a token is not knowable one product at a time. A token claimed once
carries its product; a token claimed twice carries the empty product the
unscoped read has always carried. Crediting whichever product sorts first
would have been perfectly stable and perfectly wrong: every other sharer's
tickets filed under a product that is not theirs. This is the "—" rule from
the forge-quota decision pointed at ourselves — a label that is a claim
about a ticket when it is only a fact about our config is worse than no
label, and inventing records is worse than an empty pane.

**The unscoped key is a read of its own, never a fallback the scoped reads
switch off.** It goes out whenever no product has already claimed it, and
it goes out last. Dropping it the moment some product named a token would
mean that filling in one line of `[linear]` silently emptied a backlog that
had been reading for months — the failure is invisible, because an empty
Linear source and an unconfigured one look identical on the lens. Going
last costs nothing: an issue two reads both return is dropped on the way
in, and the scoped read is merged first, so it keeps the product tag.

**Deduplication is on the issue id, not the identifier.** "ENG-124" is
unique inside a workspace and this is a list of several: two workspaces
that both key a team ENG can genuinely both raise an ENG-124, and dropping
the second as a duplicate would lose a ticket that was never seen — the one
failure a backlog must not have. That deliberately keeps a collision it
does not fix, since `ticket.id` is the identifier and `m.picked` is keyed
by it, so the two rows are ticked together by a single `space`. Keeping it
is the cheaper failure: a row you have to look at twice beats a ticket that
was never on the page.

**Reads go out together and merge in name order.** Five products must not
wait out five round-trips in series on every refresh, so the calls go
through the existing `forEach` into a pre-sized slice; the merge then walks
that slice in the order `linearReads` named it, so what a load produces
never depends on which workspace answered first. Map order is random, which
is why the naming is sorted rather than ranged.

**The token is typed where the product is named.** `[linear]` is keyed by
product name, and product names are minted in the products lens's
assignment editor — `n` there is the only thing in the product that creates
one. So `l` on that editor's products pane enters the token for the product
under the cursor, masked as it is typed and never echoed after, written
through the same copy-Save-publish path the repo assignments use. Sending
the human to `config.toml` instead meant retyping a string that has to match
a product name exactly, in another file, with silence as the only feedback
when it did not — and the failure that silence hides is the worst kind: the
token authenticates, the read succeeds, and the tickets are filed under a
product that groups nowhere. An empty entry removes the line rather than
storing a blank, because no entry and `product = ""` mean the same thing to
the reader and different things to the screen.

## Consequences

- A config naming no token is exactly the read it always was, so nothing
  that predates this has to change, and `LINEAR_API_KEY` still overrides
  the file for the unscoped key.
- There is no env override for a per-product token; they live in
  `config.toml`, which the README's "a secret can stay out of the file"
  line no longer covers for them.
- One read failing is still silence. A revoked or mistyped token now means
  one product is empty rather than the whole source, and the lens cannot
  tell that from a product with nothing assigned. The forge collector
  already decided this class of question the other way ("—" is a claim
  about us, and the cockpit says so); naming the products whose read failed
  in the backlog's sources kicker is the follow-up.
