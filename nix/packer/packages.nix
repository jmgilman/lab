{
  inputs,
  cell,
}: let
  inherit (inputs) nixpkgs std;
  l = nixpkgs.lib // builtins;
in
  with nixpkgs; {
    packerPluginLXD = buildGoModule rec {
      pname = "packer-plugin-lxd";
      version = "1.0.1";

      src = fetchFromGitHub {
        owner = "hashicorp";
        repo = "packer-plugin-lxd";
        rev = "v${version}";
        sha256 = "sha256-puJyI6Y/jE8TTmMBqX1YLuIILcLZl/Z7BTDT61QRDcQ=";
      };

      vendorSha256 = "sha256-LUw5iFUvzeSpTUpF7VkgvBrz5F+1ptzlszzPVOZln0M=";
      subPackages = ["."];
      ldflags = ["-s" "-w"];

      meta = with lib; {
        description = "Packer plugin for LXD Builder";
        homepage = "https://github.com/hashicorp/packer-plugin-lxd";
        license = licenses.mpl20;
      };
    };
    packerWithPlugins = plugins:
      buildGoModule rec {
        pname = "packer";
        version = "1.8.2";

        src = fetchFromGitHub {
          owner = "hashicorp";
          repo = "packer";
          rev = "v${version}";
          sha256 = "sha256-SaQGUVXtAI/FdqRZc4AjDkeEl9lE5i/wKsHKNGLpx8Y=";
        };

        vendorSha256 = "sha256-0GE5chSTonJFT7xomfa9a9QsnFpTFX7proo9joaDrOU=";
        subPackages = ["."];
        ldflags = ["-s" "-w"];

        nativeBuildInputs = [installShellFiles];
        postInstall = ''
          mkdir -p $out
          for plugin in ${l.concatStringsSep " " plugins}
          do
            cp $plugin/bin/* $out/bin/
          done

          installShellCompletion --zsh contrib/zsh-completion/_packer
        '';

        meta = with lib; {
          description = "A tool for creating identical machine images for multiple platforms from a single source configuration";
          homepage = "https://www.packer.io";
          license = licenses.mpl20;
        };
      };
  }
