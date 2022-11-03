{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in {
  init = nixpkgs.writeShellScriptBin "init" ''
    echo "Initializing..."
  '';
}
