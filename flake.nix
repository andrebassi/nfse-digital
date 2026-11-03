{
  # Ambiente de desenvolvimento do nfse: Go, Task, Tailwind standalone (sem
  # Node), openssl para o certificado e libxml2 (xmllint) para o Epic 2.
  description = "nfse-digital — emissor pessoal de NFS-e";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in {
      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.go-task pkgs.tailwindcss_4 pkgs.openssl pkgs.libxml2 ];
        };
      });
    };
}
