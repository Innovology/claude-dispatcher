{
  description = "Terminal dispatch cockpit for Claude Code sessions across many repositories";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        claude-dispatcher = pkgs.callPackage ./nix/package.nix { };
        default = claude-dispatcher;
      });

      overlays.default = final: _prev: {
        claude-dispatcher = final.callPackage ./nix/package.nix { };
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.golangci-lint
            pkgs.goreleaser
            pkgs.gnumake
            # The cockpit's own runtime dependencies, so `make run` works from
            # inside the shell.
            pkgs.git
            pkgs.tmux
          ];
        };
      });

      checks = forAllSystems (
        pkgs:
        let
          package = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        in
        {
          # `go test ./...` already runs as the package's check phase.
          inherit package;

          gofmt = pkgs.runCommand "claude-dispatcher-gofmt" { nativeBuildInputs = [ pkgs.go ]; } ''
            unformatted=$(cd ${package.src} && gofmt -l .)
            if [ -n "$unformatted" ]; then
              echo "gofmt needed:" >&2
              echo "$unformatted" >&2
              exit 1
            fi
            touch $out
          '';
        }
      );

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
