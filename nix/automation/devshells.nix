{
  inputs,
  cell,
}: let
  inherit (inputs) nickel nixpkgs std;
  inherit (inputs.cells.packer.packages) packerWithPlugins packerPluginLXD;
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
        nixpkgs.gopass
        nixpkgs.terraform
        nickel.packages.default
        (packerWithPlugins [packerPluginLXD])
      ];
      commands = l.map cell.lib.mkTaskCommand [
        {
          name = "init";
          help = "Initialize LXD";
          category = "LXD";
        }
      ];
    };
  }
