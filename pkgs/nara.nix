{
  lib,
  buildGoModule,
  buildNpmPackage,
  esbuild,
}:

let
  # gomarkdoc for generating API docs from Go comments
  gomarkdoc = buildGoModule {
    pname = "gomarkdoc";
    version = "1.1.0";
    src = builtins.fetchGit {
      url = "https://github.com/princjef/gomarkdoc";
      rev = "e62c5abf78916697dbc4b25f590deb28f26f5184";
      ref = "refs/tags/v1.1.0";
    };
    vendorHash = "sha256-gCuYqk9agH86wfGd7k6QwLUiG3Mv6TrEd9tdyj8AYPs=";
    doCheck = false;
  };

  web = buildNpmPackage {
    pname = "nara-web";
    version = "latest";
    src = lib.cleanSource ../.;
    #npmDepsHash = lib.fakeHash; # DO NOT REMOVE THIS COMMENT
    npmDepsHash = "sha256-VTGE+l7APw5wOyeOMP3SEIEN5z5nAv+B28f7VEZ4i18=";
    nativeBuildInputs = [ esbuild gomarkdoc ];

    makeCacheWritable = true; # necessary for git pull on mermaid dependency

    dontNpmBuild = true;

    buildPhase = ''
      runHook preBuild
      npm run build
      # Generate API docs from Go source
      ./scripts/gen-api-docs.sh
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
  vendorHash = "sha256-ePnt6SUPosm8k5bwEHoqIhOlPciIVPTrc9MWXaLpiGs=";
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
