{ lib, buildGoModule }:

buildGoModule {
  pname = "nara";
  version = "latest";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-fRM8XvpMDP0fAVY5JB9UB4qQM4q0Zk5etr6Fd5nrGd4=";
  subPackages = [ "cmd/nara" ];

  meta = with lib; {
    description = "friendly network";
    homepage = "https://github.com/eljojo/nara";
    license = licenses.mit;
    platforms = platforms.linux ++ platforms.darwin;
  };
}
