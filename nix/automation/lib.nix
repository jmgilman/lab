{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in {
  mkTask = {
    path,
    runtimeInputs ? [],
    runtimeShell ? nixpkgs.runtimeShell,
  }: let
    name = l.elemAt (l.splitString "." (l.baseNameOf path)) 0;
  in
    nixpkgs.runCommand name
    {
      inherit path;
      meta.mainProgram = name;
    }
    ''
      mkdir -p $out/bin
      script="$out/bin/${name}"

      # Create initial script with correct shebang and inputs
      echo '#!${runtimeShell}' >$script
      echo 'export PATH="${l.makeBinPath runtimeInputs}:$PATH"' >>$script

      # Append the rest of the script with original shebang removed
      sed 's/^#!.*$//' $path | tail -n +2 >>$script
      chmod +x $script

      # Validate final result
      ${nixpkgs.stdenv.shellDryRun} $script
      ${nixpkgs.shellcheck}/bin/shellcheck $script
    '';

  mkTaskCommand = {
    name,
    category,
    help,
    cell ? "automation",
    cellBlock ? "tasks",
  }: {
    inherit name help category;
    command = ''
      nix run .#${nixpkgs.system}.${cell}.${cellBlock}.${name}
    '';
  };

  mkTaskStr = cell: task: ''
    nix run .#${nixpkgs.system}.${cell}.tasks.${task}
  '';
}
