package cockpit

// seed_floor.go is the floor (triage) lens's own seed data — the design's
// representative dispatchers plus the metadata tables the detail pane renders.
// Transcribed verbatim from the v2 design source (DISPATCHES, STACKS, SAID,
// FROM, TAIL_LINES, DIFFS and the *_STYLE tables).

// dispatches is the in-flight floor, one record per DISPATCHES entry.
var dispatches = []dispatch{
	{
		feature: "stripe webhooks", repo: "cortiva-api", product: "cortiva", forge: "gh", state: "blocked", age: "4m",
		branch: branchOf("stripe webhooks"),
		why:    "It has a green PR and wants to squash-merge it. That is your call, not its call — approve and the feature is live in about six minutes.",
		signal: "approve", urgent: true, plus: 312, minus: 44, files: 9, commits: 3,
		prompt: "retry stripe webhooks with idempotency keys, and stop double-charging on replay.",
		agents: []agent{
			{"", "main", "opus", "holding at the merge gate", "idle", "blocked 4m · 41 tools"},
			{"├", "test-runner", "sonnet", "billing suite, 3 runs", "ok", "green 6m ago"},
			{"├", "webhook-replay", "sonnet", "wrote the idempotency store", "ok", "returned 9m ago"},
			{"└", "pr-writer", "haiku", "opened #151 with the changelog", "ok", "returned 5m ago"},
		},
		prs: []prRef{{"#151", "retry stripe webhooks idempotently", "0 reviews · ✓ 4/4", cGreen, "gh"}},
		runs: []runRef{
			{"CI · pnpm test", "✓ passed", cGreen, "2m"},
			{"Deploy production", "· waits for merge", cFaint, ""},
		},
		activity: []activity{
			{"Bash", "go test ./internal/billing/...", "ok", cGreen},
			{"Edit", "internal/billing/webhook.go", "", cDim},
			{"Bash", "gh pr create --fill", "#151", cGreen},
			{"Bash", "gh pr merge --squash", "blocked", cRed},
		},
		chain: []chainStep{
			{"3 commits", "+312 −44 · 9 files", "ok"},
			{"#151 open", "github · 0 reviews", "ok"},
			{"checks ✓ 4/4", "ci passed 2m ago", "ok"},
			{"merge", "waiting on you", "bad"},
			{"deploy production", "fires on merge", "idle"},
		},
		ask: &askBlock{
			kicker:   "BLOCKED ON YOU",
			headline: "cortiva-api wants to squash-merge #151. Checks are green, nobody has reviewed it.",
			evidence: "3 commits · +312 −44 · blocked 4m",
			actions:  []action{{"y", "approve merge"}, {"n", "deny"}, {"o", "open pr"}, {"enter", "attach"}},
		},
	},
	{
		feature: "tenant migration", repo: "nw-billing", product: "northwind", forge: "ado", state: "blocked", age: "12m",
		branch: branchOf("tenant migration"),
		why:    "It wants write access to infra/, which is outside the paths you allowed when you dispatched it. Widen the scope or tell it to stop there.",
		signal: "approve", urgent: true, plus: 88, minus: 12, files: 4, commits: 2,
		prompt: "split the shared tenant table per region ahead of the eu rollout.",
		agents: []agent{
			{"", "main", "opus", "waiting on a scope decision", "idle", "blocked 12m · 27 tools"},
			{"├", "schema-splitter", "sonnet", "wrote 0142_split.sql", "ok", "returned 14m ago"},
			{"└", "infra-writer", "sonnet", "blocked writing infra/terraform", "bad", "denied by scope"},
		},
		prs: []prRef{{"!322", "per-region tenant split", "draft · azure devops", cMid, "ado"}},
		runs: []runRef{
			{"Build · dotnet", "● running", cBlue, "3m"},
			{"Release-Prod", "· not reached", cFaint, ""},
		},
		activity: []activity{
			{"Read", "infra/terraform/tenants.tf", "", cDim},
			{"Edit", "db/migrations/0142_split.sql", "", cDim},
			{"Bash", "az pipelines run --name Build", "queued", cBlue},
			{"Write", "infra/terraform/eu.tf", "blocked", cRed},
		},
		chain: []chainStep{
			{"2 commits", "+88 −12 · 4 files", "ok"},
			{"!322 draft", "azure devops", "ok"},
			{"Build running", "azure pipelines", "now"},
			{"scope", "infra/ not allowed", "bad"},
			{"Release-Prod", "not reached", "idle"},
		},
		ask: &askBlock{
			kicker:   "BLOCKED ON YOU",
			headline: "nw-billing wants to write infra/terraform — outside the paths you allowed at dispatch.",
			evidence: "azure devops · blocked 12m",
			actions:  []action{{"y", "widen scope"}, {"n", "keep it out"}, {"r", "reply"}},
		},
	},
	{
		feature: "rate limiter", repo: "cortiva-api", product: "cortiva", forge: "gh", state: "claimed", age: "9m",
		branch: branchOf("rate limiter"),
		why:    "It believes the goal is met. Accept and the feature closes; reject and it reopens with your notes.",
		signal: "accept?", plus: 160, minus: 20, files: 4, commits: 2,
		prompt: "per-tenant rate limit, 429 with retry-after.",
		activity: []activity{
			{"Edit", "internal/limit/bucket.go", "", cDim},
			{"Bash", "go test -bench .", "ok", cGreen},
			{"Bash", "gh pr merge --auto", "merged", cGreen},
		},
		agents: []agent{
			{"", "main", "opus", "declared the goal met", "ok", "claimed 9m ago · 52 tools"},
			{"├", "bucket-impl", "sonnet", "token bucket per tenant", "ok", "returned 40m ago"},
			{"└", "load-check", "sonnet", "10k rps, p99 under 2ms", "ok", "returned 12m ago"},
		},
		prs:  []prRef{{"#152", "per-tenant rate limiting", "merged 9m ago", cMid, "gh"}},
		runs: []runRef{{"Deploy production", "✓ succeeded", cGreen, "6m"}},
		chain: []chainStep{
			{"2 commits", "+160 −20 · 4 files", "ok"},
			{"#152 merged", "github", "ok"},
			{"checks ✓", "bench + tests", "ok"},
			{"deployed", "live 6m ago", "ok"},
			{"close it", "accept or reject", "now"},
		},
		ask: &askBlock{
			kicker:   "CLAIMS DONE",
			headline: "cortiva-api says per-tenant rate limiting is finished, merged and live. Accept to close it.",
			evidence: "live 6m · +160 −20 · 2 commits",
			actions:  []action{{"y", "accept & close"}, {"n", "reject, reopen"}, {"r", "reply with notes"}},
		},
	},
	{
		feature: "roster csv", repo: "altsports_1", product: "altsports", forge: "gh", state: "claimed", age: "35m",
		branch: branchOf("roster csv"),
		why:    "It says the export matches the import columns. Nothing shipped yet — no PR was opened.",
		signal: "no pr", urgent: true, plus: 70, minus: 5, files: 2, commits: 1,
		prompt: "roster csv export matching the import columns exactly.",
		activity: []activity{
			{"Edit", "exporters/roster.py", "", cDim},
			{"Bash", "pytest tests/export", "ok", cGreen},
		},
		agents: []agent{{"", "main", "sonnet", "declared done without opening a pr", "ok", "claimed 35m ago · 14 tools"}},
		prs:    []prRef{},
		runs:   []runRef{{"CI · pytest", "✓ passed", cGreen, "36m"}},
		chain: []chainStep{
			{"1 commit", "+70 −5 · 2 files", "ok"},
			{"no pr", "claimed done anyway", "bad"},
			{"checks ✓", "pytest green", "ok"},
			{"merge", "—", "idle"},
			{"close it", "reject — not shipped", "now"},
		},
	},
	{
		feature: "shift rota", repo: "kalish-ops", product: "kalish", forge: "gh", state: "needs", age: "41m",
		branch: branchOf("shift rota"),
		why:    "It asked which timezone anchors the rota — venue local or account default — and has been sitting on that answer for 41 minutes.",
		signal: "41m idle", urgent: true, plus: 61, minus: 8, files: 3, commits: 1,
		prompt: "generate the weekly shift rota from availability, respect max 5 shifts.",
		agents: []agent{
			{"", "main", "sonnet", "asked a question, then stopped", "idle", "idle 41m · 12 tools"},
			{"└", "rota-solver", "sonnet", "solver passes, timezone ambiguous", "ok", "returned 43m ago"},
		},
		prs:  []prRef{},
		runs: []runRef{{"CI · rspec", "✓ passed", cGreen, "44m"}},
		activity: []activity{
			{"Read", "ops/rota/solver.rb", "", cDim},
			{"Edit", "ops/rota/solver.rb", "", cDim},
			{"Bash", "bundle exec rspec", "ok", cGreen},
		},
		chain: []chainStep{
			{"1 commit", "+61 −8 · 3 files", "ok"},
			{"no pr", "not opened yet", "idle"},
			{"checks", "—", "idle"},
			{"merge", "—", "idle"},
			{"deploy", "—", "idle"},
		},
	},
	{
		feature: "fixture import", repo: "altsports_1", product: "altsports", forge: "gh", state: "needs", age: "22m",
		branch: branchOf("fixture import"),
		why:    "Three tests are still red. It stopped rather than rewrite the parser without asking.",
		signal: "3 red", urgent: true, plus: 402, minus: 210, files: 12, commits: 5,
		prompt: "import the season fixture csv, tolerate the two legacy column orders.",
		agents: []agent{
			{"", "main", "opus", "stopped before rewriting the parser", "idle", "idle 22m · 58 tools"},
			{"├", "csv-parser", "sonnet", "two column orders handled", "ok", "returned 30m ago"},
			{"└", "test-fixer", "sonnet", "3 fixture specs still red", "bad", "gave up after 4 tries"},
		},
		prs:  []prRef{},
		runs: []runRef{{"CI · pytest", "✗ 3 failed", cRed, "22m"}},
		activity: []activity{
			{"Bash", "pytest tests/fixtures", "3 failed", cRed},
			{"Read", "importer/csv.py", "", cDim},
			{"Edit", "importer/csv.py", "", cDim},
			{"Bash", "pytest tests/fixtures", "3 failed", cRed},
		},
		chain: []chainStep{
			{"5 commits", "+402 −210 · 12 files", "ok"},
			{"no pr", "tests red", "bad"},
			{"checks ✗ 3", "pytest failing", "bad"},
			{"merge", "—", "idle"},
			{"deploy", "—", "idle"},
		},
	},
	{
		feature: "repos", repo: "claude-dispatcher", product: "dispatch", forge: "gh", state: "needs", age: "18m",
		branch: branchOf("repos"),
		why:    "The turn finished cleanly. It is waiting for your next prompt and will do nothing until it gets one.",
		signal: "18m idle", plus: 214, minus: 38, files: 6, commits: 4,
		prompt: "how does dispatcher know the list of repos to work across? there should be a way it knows... unless its scanning the disk?",
		agents: []agent{
			{"", "main", "opus", "turn complete, waiting on you", "idle", "idle 18m · 33 tools"},
			{"└", "repo-scanner", "sonnet", "walked the roots, 57 repos", "ok", "returned 20m ago"},
		},
		prs:  []prRef{},
		runs: []runRef{{"make check", "✓ passed", cGreen, "19m"}},
		activity: []activity{
			{"Bash", "rg -n \"Discover\" internal/", "", cDim},
			{"Read", "internal/repos/repos.go", "", cDim},
			{"Edit", "internal/repos/repos.go", "", cDim},
			{"Bash", "make check", "ok", cGreen},
		},
		chain: []chainStep{
			{"4 commits", "+214 −38 · 6 files", "ok"},
			{"no pr", "branch only", "idle"},
			{"checks", "make check ok", "ok"},
			{"merge", "—", "idle"},
			{"deploy", "—", "idle"},
		},
	},
	{
		feature: "invite flow", repo: "cortiva-hq", product: "cortiva", forge: "gh", state: "needs", age: "6m",
		branch: branchOf("invite flow"),
		why:    "It asked whether the legacy /invite/:token route stays alive for emails already in the wild.",
		signal: "question", plus: 140, minus: 96, files: 7, commits: 2,
		prompt: "rebuild the invite flow on signed links, one screen, no password step.",
		agents: []agent{
			{"", "main", "opus", "asked about the legacy route", "idle", "idle 6m · 21 tools"},
			{"├", "signed-links", "sonnet", "token signing + expiry", "ok", "returned 8m ago"},
			{"└", "test-fixer", "sonnet", "rewriting 2 invite specs", "now", "running 4m"},
		},
		prs:  []prRef{},
		runs: []runRef{{"CI · pnpm test", "✗ 2 failed", cRed, "6m"}},
		activity: []activity{
			{"Read", "app/invite/route.ts", "", cDim},
			{"Edit", "app/invite/page.tsx", "", cDim},
			{"Bash", "pnpm test invite", "2 failed", cRed},
			{"Edit", "app/invite/page.test.ts", "", cDim},
		},
		chain: []chainStep{
			{"2 commits", "+140 −96 · 7 files", "ok"},
			{"no pr", "waiting on you", "idle"},
			{"checks ✗ 2", "invite specs", "bad"},
			{"merge", "—", "idle"},
			{"deploy", "—", "idle"},
		},
	},
	{
		feature: "audit log filters", repo: "cortiva-hq", product: "cortiva", forge: "gh", state: "review", age: "2h",
		branch: branchOf("audit log filters"),
		why:    "Green, unreviewed, and mergeable. This is the cheapest thing you can ship today.",
		signal: "mergeable", plus: 268, minus: 41, files: 8, commits: 6,
		prompt: "add date + actor filters to the audit log, keep the url shareable.",
		agents: []agent{
			{"", "main", "opus", "done, PR open", "idle", "finished 2h ago · 46 tools"},
			{"├", "filter-ui", "sonnet", "date + actor controls", "ok", "returned 2h ago"},
			{"└", "pr-writer", "haiku", "opened #148", "ok", "returned 2h ago"},
		},
		prs: []prRef{{"#148", "audit log date + actor filters", "0 reviews · ✓ 4/4", cGreen, "gh"}},
		runs: []runRef{
			{"CI · pnpm test", "✓ passed", cGreen, "2h"},
			{"Deploy production", "· fires on merge", cFaint, ""},
		},
		activity: []activity{
			{"Edit", "app/audit/filters.tsx", "", cDim},
			{"Bash", "pnpm build", "ok", cGreen},
			{"Bash", "gh pr create --fill", "#148", cGreen},
		},
		chain: []chainStep{
			{"6 commits", "+268 −41 · 8 files", "ok"},
			{"#148 open", "0 reviews · 2h", "now"},
			{"checks ✓ 4/4", "ci green", "ok"},
			{"merge", "m to squash-merge", "now"},
			{"deploy production", "fires on merge", "idle"},
		},
	},
	{
		feature: "session replay", repo: "cortiva-api", product: "cortiva", forge: "gh", state: "review", age: "6h",
		branch: branchOf("session replay"),
		why:    "Two approvals, checks green. It only needs the merge key pressed.",
		signal: "approved", plus: 330, minus: 66, files: 11, commits: 7,
		prompt: "store session replay events behind a 30 day ttl, no pii in the payload.",
		agents: []agent{
			{"", "main", "opus", "done, approved by two humans", "idle", "finished 6h ago · 72 tools"},
			{"├", "ttl-store", "sonnet", "ttl index + reaper", "ok", "returned 6h ago"},
			{"└", "pii-scrubber", "sonnet", "payload scrub + tests", "ok", "returned 6h ago"},
		},
		prs: []prRef{{"#150", "session replay behind 30d ttl", "2 approvals · ✓ 4/4", cGreen, "gh"}},
		runs: []runRef{
			{"CI · go test", "✓ passed", cGreen, "6h"},
			{"Deploy production", "· fires on merge", cFaint, ""},
		},
		activity: []activity{
			{"Edit", "internal/replay/store.go", "", cDim},
			{"Bash", "go test ./...", "ok", cGreen},
			{"Bash", "gh pr view", "approved", cGreen},
		},
		chain: []chainStep{
			{"7 commits", "+330 −66 · 11 files", "ok"},
			{"#150 open", "2 approvals", "ok"},
			{"checks ✓ 4/4", "ci green", "ok"},
			{"merge", "m to squash-merge", "now"},
			{"deploy production", "fires on merge", "idle"},
		},
	},
	{
		feature: "invoice pdf", repo: "nw-billing", product: "northwind", forge: "ado", state: "review", age: "5h",
		branch: branchOf("invoice pdf"),
		why:    "Merged five hours ago and still not live — the Release-Prod pipeline failed on the migrate stage.",
		signal: "deploy ✗", urgent: true, plus: 190, minus: 22, files: 5, commits: 4,
		prompt: "invoice pdf with the new vat lines and a per-region footer.",
		agents: []agent{
			{"", "main", "opus", "merged, then the pipeline failed", "idle", "finished 5h ago · 38 tools"},
			{"└", "vat-lines", "sonnet", "per-region vat + footer", "ok", "returned 5h ago"},
		},
		prs: []prRef{{"!318", "invoice vat lines + region footer", "merged 5h ago", cMid, "ado"}},
		runs: []runRef{
			{"Build · dotnet", "✓ passed", cGreen, "5h"},
			{"Release-Prod · migrate", "✗ failed", cRed, "11m"},
		},
		activity: []activity{
			{"Edit", "src/Invoices/Pdf.cs", "", cDim},
			{"Bash", "dotnet test", "ok", cGreen},
			{"Bash", "az pipelines runs show", "failed", cRed},
		},
		chain: []chainStep{
			{"4 commits", "+190 −22 · 5 files", "ok"},
			{"!318 merged", "azure devops", "ok"},
			{"Build ✓", "passed", "ok"},
			{"merged", "5h ago", "ok"},
			{"Release-Prod ✗", "migrate stage · 11m", "bad"},
		},
	},
	{
		feature: "polar radar", repo: "altsports-web", product: "altsports", forge: "gh", state: "review", age: "3h",
		branch: branchOf("polar radar"),
		why:    "Checks are re-running on the second push. Nothing for you until they land.",
		signal: "ci running", plus: 512, minus: 120, files: 14, commits: 9,
		prompt: "polar radar chart for boat speed by wind angle, print-safe colours.",
		agents: []agent{
			{"", "main", "opus", "pushed again, waiting on checks", "idle", "3h · 91 tools"},
			{"├", "chart-builder", "sonnet", "polar geometry + labels", "ok", "returned 1h ago"},
			{"└", "palette-check", "haiku", "print-safe contrast pass", "ok", "returned 50m ago"},
		},
		prs:  []prRef{{"#61", "polar radar for boat speed", "1 approval · ● 2/5", cBlue, "gh"}},
		runs: []runRef{{"CI · pnpm test", "● 2 of 5 running", cBlue, "4m"}},
		activity: []activity{
			{"Edit", "src/charts/PolarRadar.tsx", "", cDim},
			{"Bash", "pnpm test", "ok", cGreen},
			{"Bash", "git push", "ok", cGreen},
		},
		chain: []chainStep{
			{"9 commits", "+512 −120 · 14 files", "ok"},
			{"#61 open", "1 approval", "ok"},
			{"checks 2/5", "running", "now"},
			{"merge", "auto on green", "idle"},
			{"deploy", "vercel", "idle"},
		},
	},
	{
		feature: "hook backoff", repo: "claude-dispatcher", product: "dispatch", forge: "gh", state: "review", age: "1h",
		branch: branchOf("hook backoff"),
		why:    "In the merge queue. Live in about four minutes with no input from you.",
		signal: "merging", plus: 74, minus: 30, files: 3, commits: 2,
		prompt: "back off the status hook when the state dir is on a network mount.",
		agents: []agent{
			{"", "main", "sonnet", "in the merge queue", "idle", "1h · 17 tools"},
			{"└", "hook-backoff", "sonnet", "exponential retry on nfs", "ok", "returned 55m ago"},
		},
		prs: []prRef{{"#77", "back off the status hook on nfs", "merge queue · ✓", cGreen, "gh"}},
		runs: []runRef{
			{"CI · make check", "✓ passed", cGreen, "1h"},
			{"Release v0.4.8", "● queued", cBlue, "1m"},
		},
		activity: []activity{
			{"Edit", "internal/hookcmd/hook.go", "", cDim},
			{"Bash", "make check", "ok", cGreen},
			{"Bash", "gh pr merge --auto", "queued", cGreen},
		},
		chain: []chainStep{
			{"2 commits", "+74 −30 · 3 files", "ok"},
			{"#77", "merge queue", "ok"},
			{"checks ✓", "ci green", "ok"},
			{"merging", "~4m", "now"},
			{"Release v0.4.8", "tags on merge", "idle"},
		},
	},
}

// stackItem is one stacked PR in a repo, bottom first — each based on the one
// below it. state: merged | ready | blocked | behind | none.
type stackItem struct{ feature, id, state, note string }

var stacks = map[string][]stackItem{
	"cortiva-api": {
		{"rate limiter", "#152", "merged", "landed 9m ago · base for the rest"},
		{"session replay", "#150", "ready", "rebased on #152 · 2 approvals, green"},
		{"stripe webhooks", "#151", "blocked", "based on #150 · merges automatically once #150 lands"},
	},
	"cortiva-hq": {
		{"audit log filters", "#148", "ready", "on main · nothing under it"},
		{"invite flow", "—", "none", "no pr yet · will stack on #148"},
		{"office", "—", "none", "working · touches the same nav, will stack third"},
	},
	"nw-billing": {
		{"invoice pdf", "!318", "merged", "merged 5h ago · deploy failed after"},
		{"tenant migration", "!322", "behind", "base moved · needs a rebase before it can merge"},
		{"billing retries", "—", "none", "working · stacks on !322"},
	},
	"claude-dispatcher": {
		{"hook backoff", "#77", "ready", "in the merge queue"},
		{"repos", "—", "none", "branch only · waiting on you"},
		{"state management", "—", "none", "working"},
		{"deploy detect", "—", "none", "working"},
	},
	"altsports_1": {
		{"roster csv", "—", "none", "claims done with no pr — nothing to stack on"},
		{"fixture import", "—", "none", "3 tests red"},
		{"kalish", "—", "none", "working"},
	},
	"altsports-web": {
		{"polar radar", "#61", "ready", "1 approval · checks re-running"},
		{"league table", "—", "none", "working"},
	},
}

// stackStateMeta mirrors STACK_STATE.
var stackStateMeta = map[string]struct{ label, color, rail, railColor string }{
	"merged":  {"merged", cMid, "│", "#2f6b41"},
	"ready":   {"ready", cGreen, "│", "#2f6b41"},
	"blocked": {"blocked", cRed, "│", "#e0554a"},
	"behind":  {"rebase", cAmber, "│", "#e0a33a"},
	"none":    {"no pr", cDim, "│", "#2a2a2a"},
}

// agentStyle mirrors AGENT_STYLE: main + subagent status glyph and colours.
var agentStyle = map[string]struct{ color, metaColor, glyph string }{
	"now":  {cWhite, cBlue, "●"},
	"ok":   {cMid, cGreen, "✓"},
	"bad":  {cMid, cRed, "✗"},
	"idle": {cDim, cFaint, "·"},
}

// chainStyle mirrors CHAIN_STYLE: the commits→pr→checks→merge→deploy nodes.
var chainStyle = map[string]struct{ color, metaColor, rule, glyph string }{
	"ok":   {cMid, cFaint, "#2a2a2a", "✓"},
	"now":  {cWhite, cMid, "#5a8fd8", "●"},
	"bad":  {cRed, cMid, "#e0554a", "■"},
	"idle": {cFaint, cFaint, "#1e1e1e", "·"},
}

// saidBy mirrors SAID: the verbatim tail of what claude last printed.
var saidBy = map[string]string{
	"stripe webhooks":   "The webhook handler is idempotent now and all four billing suites pass. I opened #151. Shall I squash-merge it?",
	"tenant migration":  "The migration splits cleanly per region, but the eu workspace needs a new terraform file under infra/ and I do not have write access there.",
	"shift rota":        "The solver respects the 5-shift cap. Before I generate the week I need to know which timezone anchors it — venue local, or the account default?",
	"fixture import":    "Both legacy column orders parse. Three fixture specs still fail on the 2019 date format. I can rewrite the parser to normalise dates first, but that touches every importer — want me to?",
	"repos":             "Discovery walks the configured scan roots three levels deep, so it is the disk, filtered by config.toml. I added an explicit repos list that takes precedence. What next?",
	"invite flow":       "Signed links work and the password step is gone. Two specs cover the old /invite/:token route — do we keep it alive for emails already sent?",
	"audit log filters": "Date and actor filters are in and the url stays shareable. Build is green, PR #148 is up.",
	"session replay":    "Events are stored behind a 30 day ttl with the payload scrubbed of pii. #150 has two approvals.",
	"invoice pdf":       "VAT lines and the per-region footer are in. !318 merged, but the Release-Prod run failed on the migrate stage — I do not have permission to re-run it.",
	"polar radar":       "Boat speed by wind angle renders correctly and the palette passes print contrast. Pushed again; checks are re-running.",
	"hook backoff":      "The hook now backs off exponentially when the state dir is on a network mount. #77 is in the merge queue.",
}

// fromBy mirrors FROM: which ticket each in-flight dispatcher came from.
var fromBy = map[string]string{
	"stripe webhooks":   "CTV-118 · linear",
	"tenant migration":  "AB#2291 · azure boards",
	"rate limiter":      "CTV-121 · linear",
	"roster csv":        "altsports_1#212 · github",
	"shift rota":        "kalish-ops#77 · github",
	"fixture import":    "altsports_1#208 · github",
	"repos":             "claude-dispatcher#61 · github",
	"invite flow":       "CTV-109 · linear",
	"audit log filters": "CTV-104 · linear",
	"session replay":    "CTV-97 · linear",
	"invoice pdf":       "AB#2274 · azure boards",
	"polar radar":       "altsports-web#41 · github",
	"hook backoff":      "claude-dispatcher#58 · github",
}

// tailLines mirrors TAIL_LINES: the live transcript preview per dispatcher.
var tailLines = map[string][]string{
	"stripe webhooks": {
		"⏺ Bash(go test ./internal/billing/...)",
		"  ⎿  ok  claude/internal/billing  1.284s",
		"⏺ The replay test passes with the idempotency key in place.",
		"⏺ Bash(gh pr create --fill)",
		"  ⎿  https://github.com/cortiva/cortiva-api/pull/151",
		"⏺ Bash(gh pr merge 151 --squash)",
		"  ⎿  ✗ permission required: merge to main",
		"⏺ I need your approval to squash-merge #151. Checks are green.",
	},
	"fixture import": {
		"⏺ Bash(pytest tests/fixtures -x)",
		"  ⎿  FAILED tests/fixtures/test_dates.py::test_2019_order",
		"⏺ Read(importer/csv.py)",
		"⏺ Update(importer/csv.py)",
		"  ⎿  Updated importer/csv.py with 14 additions and 3 removals",
		"⏺ Bash(pytest tests/fixtures)",
		"  ⎿  3 failed, 41 passed in 2.91s",
		"⏺ Normalising dates touches every importer — want me to?",
	},
}

var tailFallback = []string{
	"⏺ Read(src/index.ts)",
	"⏺ Update(src/index.ts)",
	"  ⎿  Updated src/index.ts with 22 additions and 4 removals",
	"⏺ Bash(npm test)",
	"  ⎿  ok  42 passed",
	"⏺ Continuing with the next file.",
}

// diffsBy mirrors DIFFS: the per-feature file list and hunk for the diff overlay.
var diffsBy = map[string]struct {
	files []diffFile
	hunk  []hunkLine
}{
	"stripe webhooks": {
		files: []diffFile{
			{"internal/billing/webhook.go", 142, 18},
			{"internal/billing/idempotency.go", 96, 0},
			{"internal/billing/webhook_test.go", 74, 26},
		},
		hunk: []hunkLine{
			{" ", "func (h *Handler) Receive(ctx context.Context, ev Event) error {"},
			{"-", "\tif err := h.charge(ctx, ev); err != nil {"},
			{"+", "\tkey := idempotency.From(ev.ID, ev.Attempt)"},
			{"+", "\tif seen, err := h.store.Claim(ctx, key); err != nil {"},
			{"+", "\t\treturn fmt.Errorf(\"claim %s: %w\", key, err)"},
			{"+", "\t} else if seen {"},
			{"+", "\t\treturn nil // already charged on a previous delivery"},
			{"+", "\t}"},
			{"+", "\tif err := h.charge(ctx, ev); err != nil {"},
			{" ", "\t\treturn err"},
			{" ", "\t}"},
		},
	},
}

// diffFallback mirrors DIFF_FALLBACK, shown when a feature has no recorded diff.
var diffFallback = struct {
	files []diffFile
	hunk  []hunkLine
}{
	files: []diffFile{{"src/index.ts", 22, 4}},
	hunk: []hunkLine{
		{" ", "export function run(opts: Options) {"},
		{"-", "  return execute(opts);"},
		{"+", "  const scoped = withDefaults(opts);"},
		{"+", "  return execute(scoped);"},
		{" ", "}"},
	},
}
