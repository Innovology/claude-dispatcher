#!/usr/bin/env python3
"""Build a throwaway portfolio for the README screenshots.

The cockpit reads real dispatch records, real git repos and a real config —
there is no demo mode and no seed data in the binary, deliberately. So the
screenshots are made by pointing the real binary at a fixture: fictional repos
in a temp dir, fictional records in a temp state dir, and a temp HOME holding
the config. Nothing here is compiled into the product.

Every name is invented. Do not put a real repo, product or person in this file.
"""
import hashlib, json, os, pathlib, shutil, subprocess, sys, time

ROOT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/cd-shots")
REPOS, STATE, HOME = ROOT / "repos", ROOT / "state", ROOT / "home"

PRODUCTS = {
    "acme":    ["acme-api", "acme-web", "acme-hq"],
    "bluefin": ["bluefin-core", "bluefin-web"],
    "orbit":   ["orbit-billing"],
}
UNMAPPED = ["scratch-tools", "legacy-importer"]

NOW = time.time()
def ago(mins): return NOW - mins * 60

# feature, repo, product, status, reason, pr, prstate, commits, age(min), tools, said
DISPATCHES = [
    ("seat limits", "acme-api", "acme", "needs-input", "turn complete — waiting on you",
     151, "OPEN", 3, 4, ["Read", "Edit", "Bash"],
     "Seat limits are in with a 7-day grace window. All six checks are green and nobody has reviewed it."),
    ("webhook retries", "acme-api", "acme", "blocked", "wants to write outside the allowed paths",
     0, "", 2, 9, ["Read", "Grep"],
     "I need to touch infra/terraform to add the queue. That is outside the paths you allowed at dispatch."),
    ("audit filters", "acme-web", "acme", "needs-input", "waiting for your next prompt",
     148, "OPEN", 5, 22, ["Edit", "Write"],
     "Should filter names be part of the public API? I have stopped until you decide."),
    ("invoice pdf", "orbit-billing", "orbit", "needs-input", "turn complete — waiting on you",
     0, "", 0, 31, ["Bash"],
     "The PDF renderer is missing from the CI image, so the suite cannot run here."),
    ("offline cache", "bluefin-web", "bluefin", "working", "writing components",
     0, "", 1, 2, ["Edit"], "Caching the last seven days of records for offline read."),
    ("rate limiter", "acme-api", "acme", "working", "running the suite",
     0, "", 4, 6, ["Bash"], "Running the billing suite against the new bucket."),
    ("league tiebreak", "bluefin-core", "bluefin", "working", "reading the repo",
     0, "", 0, 11, ["Grep"], "Reading the scoring rules before changing the order."),
    ("csv export", "acme-hq", "acme", "done", "deployed — live",
     129, "MERGED", 6, 180, ["Bash"], "Export shipped and the deploy is green."),
]

# What each dispatch's branch actually changed, as (path, lines written).
#
# The cockpit reads `git diff <base_sha>..<branch>` for its diff totals and for
# the hand-coding equivalent, so a fixture that only names a branch renders
# every one of those figures as zero — the screenshots would show the feature
# as absent rather than as small. The paths are chosen to exercise the file
# kinds too: source, tests, prose, and a lockfile nobody typed.
#
# A dispatch with no entry has no branch at all, which is the honest picture of
# one that has not committed anything yet.
DIFFS = {
    "seat limits":     [("internal/billing/seats.go", 320),
                        ("internal/billing/seats_test.go", 180),
                        ("docs/seat-limits.md", 40)],
    "webhook retries": [("internal/queue/retry.go", 210),
                        ("internal/queue/retry_test.go", 90)],
    "audit filters":   [("src/audit/filters.tsx", 260),
                        ("src/audit/filters.test.tsx", 140),
                        ("package-lock.json", 3000)],
    "offline cache":   [("src/cache/offline.ts", 120)],
    "rate limiter":    [("internal/limits/bucket.go", 180),
                        ("internal/limits/bucket_test.go", 90)],
    "csv export":      [("internal/export/csv.go", 430),
                        ("internal/export/csv_test.go", 220),
                        ("docs/export.md", 60)],
}

# The backlog: open issues assigned to you, as `gh search issues` returns them.
# The cockpit asks gh for them, so the fixture puts a stand-in gh first on the
# session's PATH (see write_gh) that answers that one search and fails every
# other call, which is how a forge with nothing to say for these repos reads.
#
# repo, number, title, labels, filed (days ago), last touched (days ago), body
BACKLOG = [
    ("acme-api", 214, "Seat limits ignore the grace window on downgrade", ["bug", "billing"], 12, 1,
     "Downgrading a plan drops seats at once instead of after the 7-day grace window."),
    ("acme-api", 209, "Rate limiter counts retries twice", ["bug"], 20, 6,
     "A retried request is charged against the bucket a second time."),
    ("acme-api", 201, "Webhook signatures fail after key rotation", ["p0", "security"], 3, 0,
     "Deliveries signed with the previous key are rejected during the overlap window."),
    ("acme-api", 188, "Paginate the audit log endpoint", ["api"], 41, 15,
     "The endpoint returns every event at once; large tenants time out."),
    ("acme-api", 176, "Document the seat API errors", ["docs"], 60, 33,
     "Each error code needs an example and what to do about it."),
    ("acme-api", 162, "Move invoice numbering to a sequence", ["billing"], 75, 40,
     "Numbers are derived from a count and collide under load."),
    ("acme-web", 97, "Audit filters lose their state on reload", ["bug", "ui"], 9, 2,
     "Filters reset to defaults whenever the page is refreshed."),
    ("acme-web", 93, "Dark mode for the billing pages", ["ui"], 30, 21,
     "The billing pages still render light whatever the theme says."),
    ("acme-web", 88, "Keyboard shortcut for the seat table", ["ui", "a11y"], 44, 30,
     "The seat table cannot be reached from the keyboard."),
    ("acme-web", 81, "Empty state for teams with no invoices", ["ui"], 52, 41,
     "A new team sees a blank table and no explanation."),
    ("acme-web", 77, "Customer report: export button does nothing on Safari", ["customer", "bug"], 6, 1,
     "Clicking export on Safari 18 shows no download and no error."),
    ("acme-hq", 41, "Weekly digest goes out twice on DST change", ["bug"], 18, 9,
     "The scheduler runs the job once per local hour, and the hour repeats."),
    ("acme-hq", 38, "Retire the legacy CSV importer", ["chore"], 90, 70,
     "Nothing has called it since the new importer shipped."),
    ("bluefin-core", 330, "League tiebreak ignores head-to-head", ["bug", "scoring"], 5, 1,
     "Teams level on points are ordered by goal difference before head-to-head."),
    ("bluefin-core", 327, "Fixture generator repeats home games", ["scoring"], 14, 8,
     "Some teams get three home games in a row."),
    ("bluefin-core", 319, "Cache standings per round", ["perf"], 28, 20,
     "Standings are recomputed on every request."),
    ("bluefin-core", 305, "Season rollover keeps last season's byes", ["bug"], 35, 12,
     "Byes from the previous season carry into the new fixture list."),
    ("bluefin-core", 298, "Expose standings as JSON", ["api"], 48, 44,
     "Partners scrape the HTML table today."),
    ("bluefin-web", 145, "Offline mode shows stale scores", ["bug", "offline"], 7, 3,
     "Cached scores are shown without saying how old they are."),
    ("bluefin-web", 141, "Install prompt on Android", ["ui"], 22, 18,
     "The app never offers to be installed."),
    ("bluefin-web", 136, "Translate the fixtures page", ["i18n"], 39, 26,
     "Dates and team names are hard-coded in English."),
    ("bluefin-web", 129, "Score ticker flickers on slow phones", ["perf", "ui"], 57, 50,
     "Every update re-renders the whole ticker."),
    ("orbit-billing", 66, "Invoice PDF missing the VAT number", ["urgent", "billing"], 2, 0,
     "Invoices for EU customers are not compliant without it."),
    ("orbit-billing", 63, "Proration rounds the wrong way", ["bug", "billing"], 16, 10,
     "Mid-cycle upgrades are charged one cent too much."),
    ("orbit-billing", 58, "Dunning emails ignore the customer's locale", ["i18n"], 33, 29,
     "Every reminder is sent in English."),
    ("orbit-billing", 51, "Refunds report for finance", ["report"], 64, 60,
     "Finance rebuilds this by hand every month."),
    ("acme-api", 155, "Seat API returns 500 on an empty team", ["bug"], 8, 4,
     "Listing seats for a team with no members throws instead of returning []."),
    ("acme-api", 149, "Trim the audit payload", ["perf"], 70, 62,
     "Every event carries the whole request body."),
    ("acme-web", 72, "Billing settings link is broken in the footer", ["bug", "billing"], 4, 2,
     "The footer still points at the old billing route."),
    ("acme-hq", 35, "Archive closed workspaces after a year", ["chore"], 120, 110,
     "Nobody has opened most of them since they closed."),
    ("bluefin-core", 290, "Ranking export for the federation", ["report"], 66, 58,
     "The federation wants the table as a spreadsheet each month."),
    ("bluefin-web", 122, "Team page loads every past season", ["perf"], 61, 55,
     "A club with twenty seasons downloads all of them up front."),
    ("orbit-billing", 47, "Retry failed card charges with backoff", ["billing"], 45, 31,
     "A declined charge is retried every hour, forever."),
    ("scratch-tools", 12, "Script to rotate staging keys", ["chore"], 25, 25,
     "Rotation is a page of manual steps today."),
    ("scratch-tools", 9, "Lint the deploy scripts", ["chore"], 80, 79,
     "Two of them fail shellcheck."),
    ("legacy-importer", 7, "Importer chokes on BOM-prefixed files", ["bug"], 100, 95,
     "Files saved from Excel start with a byte-order mark."),
]


def write_gh(root):
    """Write the stand-in gh into root/bin, and the issues it answers with."""
    def iso(days):
        return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(NOW - days * 86400))
    issues = [{
        "repository": {"name": repo, "nameWithOwner": f"example/{repo}"},
        "number": n, "title": title, "url": f"https://example.invalid/{repo}/issues/{n}",
        "body": body, "state": "open", "labels": [{"name": l} for l in labels],
        "createdAt": iso(filed), "updatedAt": iso(touched),
    } for repo, n, title, labels, filed, touched, body in BACKLOG]
    bin_dir = root / "bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    (bin_dir / "issues.json").write_text(json.dumps(issues))
    gh = bin_dir / "gh"
    gh.write_text('#!/bin/sh\ncase "$*" in\n  "search issues"*) cat "$(dirname "$0")/issues.json" ;;\n'
                  '  *) exit 1 ;;\nesac\n')
    gh.chmod(0o755)


def sh(*a, cwd=None):
    subprocess.run(a, cwd=cwd, check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

def make_repo(name):
    d = REPOS / name
    d.mkdir(parents=True, exist_ok=True)
    sh("git", "init", "-q", "-b", "main", cwd=d)
    sh("git", "config", "user.email", "dev@example.com", cwd=d)
    sh("git", "config", "user.name", "Dev", cwd=d)
    (d / "README.md").write_text(f"# {name}\n")
    sh("git", "add", "-A", cwd=d)
    sh("git", "commit", "-q", "-m", "init", cwd=d)
    return d

def make_branch(repo, branch, files):
    """Cut the dispatch's feature branch, put a real diff on it, and return the
    SHA it was cut from — which is what the record's base_sha has to be.

    It leaves the repo back on main, because several dispatches share one repo
    and each needs its branch cut from the same place. Nothing here is a
    worktree: the screenshots only need the refs to exist, and the real
    dispatcher's worktrees are not what is being pictured.
    """
    base = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, check=True,
                          capture_output=True, text=True).stdout.strip()
    if not files:
        return base
    sh("git", "checkout", "-q", "-b", branch, cwd=repo)
    for path, lines in files:
        p = repo / path
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text("".join(f"// {path} line {i}\n" for i in range(lines)))
    sh("git", "add", "-A", cwd=repo)
    sh("git", "commit", "-q", "-m", branch, cwd=repo)
    sh("git", "checkout", "-q", "main", cwd=repo)
    return base


def transcript(path, tools, said):
    """A Claude Code JSONL tail: tool_use blocks then the assistant's message."""
    rows = []
    for t in tools:
        rows.append({"type": "assistant", "message": {"role": "assistant",
                     "content": [{"type": "tool_use", "name": t}]}})
    rows.append({"type": "assistant", "message": {"role": "assistant",
                 "content": [{"type": "text", "text": said}]}})
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(json.dumps(r) for r in rows) + "\n")

def main():
    shutil.rmtree(ROOT, ignore_errors=True)
    for d in (REPOS, STATE / "dispatches", HOME / ".config" / "claude-dispatcher"):
        d.mkdir(parents=True, exist_ok=True)

    names = [r for rs in PRODUCTS.values() for r in rs] + UNMAPPED
    paths = {n: make_repo(n) for n in names}

    def iso(t): return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime(t)) + "Z"

    for i, (feat, repo, prod, status, reason, pr, prst, ncom, age, tools, said) in enumerate(DISPATCHES):
        slug = feat.replace(" ", "-")
        tp = STATE / "transcripts" / f"{slug}.jsonl"
        transcript(tp, tools, said)
        # The working view reads "last output" from the transcript's mtime, so
        # age the file too — otherwise every dispatcher looks like it spoke a
        # moment ago, whatever its record says.
        os.utime(tp, (ago(age), ago(age)))
        base = make_branch(paths[repo], f"feature/{slug}", DIFFS.get(feat, []))
        rec = {
            "id": f"fix{i:04d}", "feature": feat, "slug": slug,
            "repo_path": str(paths[repo]), "repo_name": repo, "product": prod,
            "branch": f"feature/{slug}", "base_sha": base, "prompt": said,
            "tmux_session": f"disp-{slug}", "transcript_path": str(tp),
            "commits": ["0" * 40] * ncom,
            "status": status, "status_reason": reason,
            "created_at": iso(ago(age + 120)), "updated_at": iso(ago(age)),
        }
        if pr:
            rec.update({"pr_number": pr, "pr_state": prst,
                        "pr_url": f"https://example.invalid/pr/{pr}"})
        if prst == "MERGED":
            rec["pr_merged_at"] = iso(ago(age))
            rec["deployed_at"] = iso(ago(age - 5))
        (STATE / "dispatches" / f"{rec['id']}.json").write_text(json.dumps(rec, indent=2))

    # The usage lens: Claude Code's own limit figures, as its status line hands
    # them over (the reading `claude-dispatcher statusline` keeps per config
    # dir), and a week of token usage in the default login's transcripts.
    claude_dir = str(HOME / ".claude")
    key = hashlib.sha256(claude_dir.encode()).hexdigest()[:16]
    (STATE / "accounts").mkdir(parents=True, exist_ok=True)
    (STATE / "accounts" / f"{key}.json").write_text(json.dumps({
        "config_dir": claude_dir,
        "five_hour": {"used_percentage": 38, "resets_at": int(NOW + 2 * 3600 + 900)},
        "seven_day": {"used_percentage": 41, "resets_at": int(NOW + 2 * 86400 + 5 * 3600)},
        "at": iso(ago(1)),
    }, indent=2))
    used = HOME / ".claude" / "projects" / "fixture" / "week.jsonl"
    used.parent.mkdir(parents=True, exist_ok=True)
    rows = []
    for h, model, tok in [(1, "claude-opus-4", 900_000), (3, "claude-sonnet-4", 2_400_000),
                          (30, "claude-opus-4", 6_100_000), (60, "claude-sonnet-4", 9_800_000),
                          (100, "claude-haiku-4", 700_000)]:
        rows.append({"type": "assistant", "timestamp": iso(ago(h * 60)), "session_id": f"s{h}",
                     "message": {"model": model, "usage": {"input_tokens": tok // 4, "output_tokens": tok // 4,
                                 "cache_creation_input_tokens": tok // 2}}})
    used.write_text("\n".join(json.dumps(r) for r in rows) + "\n")

    cfg = [f'roots = ["{REPOS}"]', "", "[products]"]
    for p, rs in PRODUCTS.items():
        cfg.append(f'{p} = [{", ".join(chr(34)+r+chr(34) for r in rs)}]')
    (HOME / ".config" / "claude-dispatcher" / "config.toml").write_text("\n".join(cfg) + "\n")
    write_gh(ROOT)

    print(f"fixture: {len(names)} repos, {len(DISPATCHES)} dispatches, {len(BACKLOG)} issues -> {ROOT}")

if __name__ == "__main__":
    main()
