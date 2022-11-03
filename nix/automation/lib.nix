{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in {
  mkTaskStr = cell: task: ''
    nix run .#${nixpkgs.system}.${cell}.tasks.${task}
  '';
}
