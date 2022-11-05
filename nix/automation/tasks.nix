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
      # Check that LXD is available
      if ! command -v lxd &> /dev/null; then
        echo "LXD must be enabled before running initialization"
        echo "Please configure the appropriate modules on NixOS first"
        exit 1
      else
        echo "LXD found..."
      fi

      # Attempt to discern if it's been initialized
      pools=$(lxc storage list -f json | ${nixpkgs.jq}/bin/jq ". | length")
      if [[ $pools -lt 1 ]]; then
        echo "LXD appears to be uninitialized."
        echo "Initializing..."
        lxd init --preseed "${inputs.self + "/lxd/conf.yaml"}"
      else
        echo "LXD is initialized..."
      fi

      echo "Initialization complete!"
    '';
  };
}
