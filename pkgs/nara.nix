{
  lib,
  buildGoModule,
  buildNpmPackage,
  esbuild,
}:

let
  web = buildNpmPackage {
    pname = "nara-web";
    version = "latest";
    src = lib.cleanSource ../.;
    # npmDepsHash = lib.fakeHash;
    npmDepsHash = "sha256-GxmC9gIeq1rh9jX4fYVCkJQsLW5SEvAYZt73/IbYpmM=";
    nativeBuildInputs = [ esbuild ];

    dontNpmBuild = true;

    buildPhase = ''
      runHook preBuild
      npm run build
      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      mkdir -p $out
      cp nara-web/public/app.js $out/app.js
      cp nara-web/public/app.css $out/app.css
      cp nara-web/public/vendor.css $out/vendor.css
      runHook postInstall
    '';
  };
in
buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-ZlI5zbq3CzaoRgrR/lYv5lv2leipFxQHT5shV6BzrBg=";
  subPackages = [ "cmd/nara" ];

  preBuild = ''
    echo "Copying prebuilt web assets..."
    cp ${web}/app.js nara-web/public/app.js
    cp ${web}/app.css nara-web/public/app.css
    cp ${web}/vendor.css nara-web/public/vendor.css
  '';

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
