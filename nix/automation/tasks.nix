{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in {
  init = cell.lib.mkTask {
    path = inputs.self + /nix/automation/tasks/init.sh;
    runtimeInputs = [nixpkgs.jq];
  };
}
