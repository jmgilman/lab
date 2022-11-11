{
  inputs.nixpkgs.url = "nixpkgs";

  inputs.std.url = "github:divnix/std";
  inputs.std.inputs.nixpkgs.follows = "nixpkgs";

  inputs.utils.url = "nix-utils";
  inputs.utils.inputs.nixpkgs.follows = "nixpkgs";
  inputs.utils.inputs.std.follows = "std";

  inputs.nickel.url = "github:tweag/nickel";
  inputs.nickel.inputs.nixpkgs.follows = "nixpkgs";

  outputs = {std, ...} @ inputs:
    std.growOn
    {
      inherit inputs;
      cellsFrom = ./nix;

      cellBlocks = [
        (std.blockTypes.data "constants")
        (std.blockTypes.devshells "devshells")
        (std.blockTypes.functions "lib")
        (std.blockTypes.installables "packages")
        (std.blockTypes.nixago "configs")
        (std.blockTypes.runnables "tasks")
      ];
    }
    {
      devShells = std.harvest inputs.self ["automation" "devshells"];
    };
}
