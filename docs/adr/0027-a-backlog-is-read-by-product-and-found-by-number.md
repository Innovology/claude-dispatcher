# A backlog is read by product and found by its number

## Status

Accepted (2026-10-30)

## Context

The backlog lens gathers the open tickets assigned to you from GitHub Issues,
Linear and Azure Boards into one list you can dispatch from. That list was one
undifferentiated table with only a source filter, and it was drawn from the top
of the pane until it ran out of room. Reported as *"the backlog of the
dispatcher doesn't scroll through the issues as the list outgrows the height of
the page. there's no filtering, which would be useful in a large project"*, with
the shape of the fix asked for in the same breath: *grouped by the product
defined in the products*, then *search by the issue number (#NNN in case of gh
issues and linear has something similar too), fuzzily by the title or the
contents of the issue*, and *sort by date filed, by staleness, by label*.

Three things stood in the way:

- **Nothing scrolled.** A table longer than the pane stopped at its edge, read as
  the whole of the backlog, and `j` walked the cursor off into rows nobody could
  see. `enter` then dispatched a ticket that was not on the screen.
- **There was nothing to sort by.** The GitHub search asked for `updatedAt`
  only; Linear read no labels, so its only "label" was its state; an Azure item
  carried no date at all and showed a blank age.
- **The cursor was an index.** Any re-sort or filter would have left it on a
  different ticket, and `enter` dispatches what the cursor is on.

## Decision

**The table is windowed on the cursor, and says where it was cut.** The cursor's
line is kept in the middle of the window, as the dispatch form's lists already
do. The top line of a cut table counts the tickets above it, and the bottom line
counts those below. The top line also names the product on screen whenever that
product's divider has scrolled away. `g`/`G` go to either end, and `pgdown`/
`pgup` move a screen.

**Tickets are grouped by product, in the products lens's order.** A product is
how this cockpit groups everything, so the backlog uses the same grouping, under
a divider naming each product with its count. Using the products lens's order
means lens 2 and lens 3 list products the same way. After those come any product
that lens does not list, such as one a Linear token names with no repos, and
last comes `unassigned` for what no product claims, as everywhere else. `p`
narrows the table to one product. The header reads "12 of 340 open" while
anything narrows the table, so a filter left on is never mistaken for the size
of the backlog. An empty table says what emptied it: an empty backlog, a filter,
and a search that found nothing each get their own sentence.

**`o` orders each group, and the header names which end comes first.** The
orders are priority (the default), newest filed, stalest, label and repo, and
`O` turns any of them round. The header says "newest filed first" rather than
"filed", because "filed" alone does not say whether new or old is on top. Ties
go to the more urgent ticket, then to the one that has waited longest, then to
the order the sources gave, so a redraw never reshuffles tickets that tie. **A
ticket missing an order's figure comes last whichever way round the order is**:
reversing must not fill the top of the table with the tickets the order can say
nothing about. The table's last column shows the figure the order sorts by:
FILED, LABELS, or otherwise IDLE, which is what the column always showed.

The sources carry what that needs. `createdAt` is one more field in the same
single GitHub search, Linear adds `createdAt` and `labels`, and Azure adds
`System.CreatedDate` and `System.ChangedDate`. A ticket keeps its labels
(`tags`) apart from the line the detail pane prints, which for Linear and Azure
leads with the state.

**`/` filters the table, and a ticket is found by its number first.** The
matcher is the one decisions already use, lifted into `textsearch.go` rather
than copied: words apart must all match, smart-case, and each word matches in
one of three ways.

- **By id.** `#123`, `123` and `0123` all name GitHub issue 123. `eng-124` and
  `124` both name Linear's ENG-124, and `ab#42` and `42` both name Azure's AB#42.
  Every source's id ends in its number, and that number is what a person says
  aloud. An exact match ranks above every other hit. A number that a ticket's
  begins with ranks one step below, because a query is typed a digit at a time:
  `#12` finds 12 and then 120…129, instead of nothing until the last digit
  lands.
- **In the id and title, fuzzily**, as fzf matches a line.
- **In the body and labels, fuzzily inside one word.** Kept inside one word, a
  fuzzy match stops matching nearly any query, as measured on decisions.

**A word that starts with `#` is a number and nothing else.** By typing the sign
that means "issue number", the reader said what they meant, so `#2` must not
also find every body that mentions a 2. This is an option on the shared matcher
(`textIDs.only`); decisions do not use it.

The table is filtered, not lit in place as the products lens's search is. A
backlog of hundreds is searched to find the dozen it leaves. A hit found in its
body or labels quotes the matching words on a line under it, because its title
cannot show why it is there. **Hits stay grouped by product, best first, and the
groups go by their best hit.** Otherwise `#123` would put the 1230s of the first
product above the one ticket it names, in the third, and the cursor, which a
search puts on the top row, would sit on a ticket that is not the answer. `o`
re-sorts the hits under an order of the search's own, so esc hands back the
order you were browsing in. While the query is typed it has the keyboard, as on
decisions. A load re-runs the query, because a hit names a ticket by its place in
the backlog.

**The cursor is held by the ticket's id**, as triage's is (`fleetSync`), through
every re-sort, filter, search and load. When its ticket leaves the table, the
cursor lands on whatever now sits at its place and is held by that ticket from
then on.

**The backlog's keys are a keymap scope of their own, layered on the list
scope.** `backlog.order`, `.reverse`, `.product`, `.source`, `.first`, `.last`,
`.page.down` and `.page.up` resolve after the `list.*` keys on the same screen.
The keymap therefore refuses a key bound in both scopes and names both actions,
the same rule that already stops a lens key from reusing a global one. `s` was a
literal the keymap did not know, and is now `backlog.source`.

## Consequences

- The backlog still reads what its sources return: GitHub's search stops at
  100 issues and Linear's read at 50 per token. A larger backlog is only partly
  on the table, and nothing here says so. Raising those limits costs forge
  quota (one GraphQL request per hundred issues), so it is a separate decision.
- Two workspaces' identical identifiers still share one picked mark (ADR 0006),
  and now share one cursor key. The first is found, which is the same cost
  `space` already had.
- The decisions search moved to the shared matcher unchanged; its tests pass
  without edits beyond the renamed types.
- The README's backlog screenshot was an empty table. The screenshot fixture
  now puts a stand-in `gh` first on the session's PATH. It answers the issue
  search and fails every other call, so the shots show a real render of
  invented tickets and never read a real forge.
