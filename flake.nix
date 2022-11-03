{
  inputs.std.url = "github:divnix/std";
  inputs.nixpkgs.url = "nixpkgs";

  outputs = { std, ... } @ inputs:
    std.growOn
      {
        inherit inputs;
        cellsFrom = ./nix;

        cellBlocks = [
          (std.blockTypes.devshells "devshells")
        ];
      }
      {
        devShells = std.harvest inputs.self [ "automation" "devshells" ];
      };
}
