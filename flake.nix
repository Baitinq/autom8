{
  description = "autom8 - CLI tool to automate AI agent workflows";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            go-tools
            claude-code
            codex
          ];
        };

        packages.default = pkgs.buildGoModule {
          pname = "autom8";
          version = "0.1.0";
          src = ./.;
          vendorHash = null;
        };
      });
}
