{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in
  l.mapAttrs (_: std.lib.dev.mkShell) {
    default = {...}: {
      name = "lab devshell";
      imports = [std.std.devshellProfiles.default];
      nixago = [
        cell.configs.conform
        cell.configs.lefthook
        cell.configs.prettier
        cell.configs.treefmt
      ];
      packages = [
        nixpkgs.ansible
        nixpkgs.terraform
      ];
    };
  }
