{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in {
  init = nixpkgs.writeShellApplication {
    name = "init";
    text = ''
      # This is a destructive command, so double-check...
      read -p "Are you sure you want to intiailize LXD? " -n 1 -r
      echo
      if [[ ! $REPLY =~ ^[Yy]$ ]]
      then
          exit 1
      fi

      # Initialize LXD
      lxd init --preseed "${inputs.self + "/lxd/conf.yaml"}"
    '';
  };
}
