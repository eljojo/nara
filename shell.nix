with (import <nixpkgs> {});
let
  ruby = ruby_3_0;
  env = bundlerEnv {
    name = "nara-web-bundler-env";
    inherit ruby;
    gemdir = ./.;
  };
in stdenv.mkDerivation {
  name = "nara-web-dev";
  buildInputs = [ ruby nodejs bundix env (lowPrio env.wrappedRuby) ];
}
