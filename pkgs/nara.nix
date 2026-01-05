{ lib, buildGoModule }:

buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-sRKtQ4Tn0ZarT+/TREuJqyYW/KOWELREenQiToFE054=";
  subPackages = [ "cmd/nara" ];

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
