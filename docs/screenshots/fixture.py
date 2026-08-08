#!/usr/bin/env python3
"""Build a throwaway portfolio for the README screenshots.

The cockpit reads real dispatch records, real git repos and a real config —
there is no demo mode and no seed data in the binary, deliberately. So the
screenshots are made by pointing the real binary at a fixture: fictional repos
in a temp dir, fictional records in a temp state dir, and a temp HOME holding
the config. Nothing here is compiled into the product.

Every name is invented. Do not put a real repo, product or person in this file.
"""
import json, os, pathlib, shutil, subprocess, sys, time

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
        rec = {
            "id": f"fix{i:04d}", "feature": feat, "slug": slug,
            "repo_path": str(paths[repo]), "repo_name": repo, "product": prod,
            "branch": f"feature/{slug}", "prompt": said,
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

    cfg = [f'roots = ["{REPOS}"]', "", "[products]"]
    for p, rs in PRODUCTS.items():
        cfg.append(f'{p} = [{", ".join(chr(34)+r+chr(34) for r in rs)}]')
    (HOME / ".config" / "claude-dispatcher" / "config.toml").write_text("\n".join(cfg) + "\n")

    print(f"fixture: {len(names)} repos, {len(DISPATCHES)} dispatches -> {ROOT}")

if __name__ == "__main__":
    main()
