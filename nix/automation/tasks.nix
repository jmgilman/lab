{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  inherit (inputs.utils) tasks;
  l = nixpkgs.lib // builtins;
in {
  init = tasks.lib.mkScriptTask {
    name = "init";
    category = "LXD";
    help = "Initialize LXD";
    path = inputs.self + /nix/automation/tasks/init.sh;
    runtimeInputs = [nixpkgs.jq];
  };
}
