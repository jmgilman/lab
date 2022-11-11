{
  inputs,
  cell,
}: let
  inherit (inputs) nickel nixpkgs std utils;
  inherit (inputs.cells.packer.packages) packerWithPlugins packerPluginLXD;
  inherit (inputs.utils) tasks;
  l = nixpkgs.lib // builtins;

  # Convert tasks into devshell commands
  taskCommands = l.mapAttrsToList (_: task: tasks.lib.mkTaskCommand {inherit task;}) cell.tasks;
in
  l.mapAttrs (_: std.lib.dev.mkShell) {
    default = {...}: {
      name = "lab devshell";
      imports = [
        (utils.devshell.profiles.core {})
        (utils.devshell.profiles.format {})
      ];
      packages = [
        nixpkgs.ansible
        nixpkgs.gopass
        nixpkgs.terraform
        nickel.packages.default
        (packerWithPlugins [packerPluginLXD])
      ];
      commands = [] ++ taskCommands;
    };
  }
