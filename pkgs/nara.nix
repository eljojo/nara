{ lib, buildGoModule }:

buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-Bz6wq2zF9h9fKfP4bvKk8P4GQs60gVsyaNp4alF7L1M=";
  subPackages = [ "cmd/nara" ];

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
