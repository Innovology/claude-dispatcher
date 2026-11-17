{
  lib,
  buildGoModule,
  makeWrapper,
  git,
  tmux,
  # Releases are stamped by goreleaser from the git tag; a flake build of an
  # arbitrary commit is honestly a dev build, exactly like `make install`.
  # Override to stamp one: `pkgs.claude-dispatcher.override { version = "2.3.1"; }`.
  version ? "dev",
}:

let
  # A Go test's fixtures live in a directory named testdata beside its
  # package, whatever the files in it are called, so they are found by the
  # directory's name — and fileFilter cannot do that, because its predicate
  # sees a file's own name and never the directories above it. This walks the
  # tree for them instead, skipping what the go tool skips: a directory whose
  # name starts with "." or "_" holds no package whose tests could read one.
  testdataDirs =
    dir:
    lib.concatLists (
      lib.mapAttrsToList (
        name: type:
        if type != "directory" || lib.hasPrefix "." name || lib.hasPrefix "_" name then
          [ ]
        else if name == "testdata" then
          [ (dir + "/${name}") ]
        else
          testdataDirs (dir + "/${name}")
      ) (builtins.readDir dir)
    );
in
buildGoModule {
  pname = "claude-dispatcher";
  inherit version;

  # Only what the build and its tests read decides the derivation — the Go
  # sources and the tests' fixtures — so editing the README or the workflows
  # does not invalidate it.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions (
      [
        ../go.mod
        ../go.sum
        (lib.fileset.fileFilter (f: f.hasExt "go") ../.)
      ]
      ++ testdataDirs ../.
    );
  };

  vendorHash = "sha256-DZ7Iz/MbDeoYMWfH/Dfe49gOtG6b/Yv5t6zNxRbvQFg=";

  env.CGO_ENABLED = "0";

  # -trimpath is already buildGoModule's default; -s -w matches .goreleaser.yml.
  ldflags = [
    "-s"
    "-w"
    "-X"
    "claude-dispatcher/internal/version.Version=${version}"
  ];

  nativeBuildInputs = [ makeWrapper ];

  # The dispatch and ship tests shell out to git; nothing in the suite needs
  # tmux (the supervisor tests only assert the backend's name).
  nativeCheckInputs = [ git ];

  # git and tmux are hard runtime dependencies — tmux is the process
  # supervisor, git cuts the per-dispatch branch and worktree. --suffix, not
  # --prefix, so a user's own git still wins on PATH. `claude` and `gh` stay
  # the user's to provide: the point of the cockpit is to drive the Claude
  # Code install they already run.
  postInstall = ''
    wrapProgram $out/bin/claude-dispatcher \
      --suffix PATH : ${
        lib.makeBinPath [
          git
          tmux
        ]
      }
  '';

  meta = {
    description = "Terminal dispatch cockpit for Claude Code sessions across many repositories";
    homepage = "https://github.com/Innovology/claude-dispatcher";
    license = lib.licenses.mit;
    mainProgram = "claude-dispatcher";
    # The tmux supervisor is unix-only; the Windows build has its own backend
    # but nix does not target it.
    platforms = lib.platforms.unix;
  };
}
