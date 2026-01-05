{
  description = "friendly network";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    utils.url = "github:numtide/flake-utils";
    flake-compat = {
      url = "github:edolstra/flake-compat";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, utils, ... }:
    utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        nara = pkgs.callPackage ./pkgs/nara.nix { };
      in
      {
        packages = {
          default = nara;
          nara = nara;
          docker = pkgs.dockerTools.buildLayeredImage {
            name = "nara";
            tag = "latest";
            contents = [ nara pkgs.cacert ];
            config = {
              Cmd = [ "${nara}/bin/nara" "-serve-ui" "-http-addr" ":8080" ];
              Env = [ "HTTP_ADDR=:8080" ];
              ExposedPorts = {
                "8080/tcp" = { };
              };
            };
          };
        };
      }) // {
      nixosModules.default = self.nixosModules.nara;
      nixosModules.nara = import ./nara.nix;
    };
}
