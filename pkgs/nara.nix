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
    #npmDepsHash = lib.fakeHash;
    npmDepsHash = "sha256-3+l4TV3iNxaw1Hu0I3ogHcSuZv9ZRj/Zlla/YbFFYgQ=";
    nativeBuildInputs = [ esbuild ];

    dontNpmBuild = true;

    buildPhase = ''
      runHook preBuild
      npm run build
      ./node_modules/.bin/astro build --root docs --config astro.config.mjs
      mkdir -p nara-web/public/docs
      cp -r docs/dist/. nara-web/public/docs
      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      mkdir -p $out/docs
      cp nara-web/public/app.js $out/app.js
      cp nara-web/public/app.css $out/app.css
      cp nara-web/public/vendor.css $out/vendor.css
      cp -r nara-web/public/docs/. $out/docs/
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
    mkdir -p nara-web/public/docs
    cp -r ${web}/docs/. nara-web/public/docs
  '';

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
