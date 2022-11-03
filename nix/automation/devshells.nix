{ inputs
, cell
}:
let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in
l.mapAttrs (_: std.lib.dev.mkShell) {
  default = { ... }: {
    name = "lab devshell";
    imports = [ std.std.devshellProfiles.default ];
    packages = [
      nixpkgs.ansible
      nixpkgs.terraform
    ];
  };
}
