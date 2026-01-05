{ lib, buildGoModule }:

buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-XqceohRq03OMny8VX1RHBRYtY68VHVG9K2KVa5Ra4a8=";
  subPackages = [ "cmd/nara" ];

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
