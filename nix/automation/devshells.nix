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
        nixpkgs.nickel
        nixpkgs.terraform
      ];
      commands = [
        {
          name = "init";
          command = cell.lib.mkTaskStr "automation" "init";
          help = "Initialize LXD";
          category = "LXD";
        }
      ];
    };
  }
