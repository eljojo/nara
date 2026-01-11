{ lib, buildGoModule }:

buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-ZlI5zbq3CzaoRgrR/lYv5lv2leipFxQHT5shV6BzrBg=";
  subPackages = [ "cmd/nara" ];

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
