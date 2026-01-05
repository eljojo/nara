{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.nara;
in
{
  options.services.nara = {
    enable = lib.mkEnableOption "nara friendly network";

    package = lib.mkOption {
      type = lib.types.package;
      default = (import ./default.nix).packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "The nara package to use.";
    };

    instances = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "List of nara instance names to run. If empty and enabled, runs one instance with the hostname.";
    };

    mqttHost = lib.mkOption {
      type = lib.types.str;
      default = "tcp://hass.eljojo.casa:1883";
      description = "MQTT broker address.";
    };

    extraArgs = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Extra arguments to pass to the nara binary.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      description = "Environment file containing MQTT_USER and MQTT_PASS.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.nara = {
      isSystemUser = true;
      group = "nara";
    };
    users.groups.nara = { };

    systemd.services = 
      let 
        instances = if cfg.instances == [] then [ "" ] else cfg.instances;
        makeService = name: {
          name = if name == "" then "nara" else "nara-${name}";
          value = {
            wantedBy = [ "multi-user.target" ];
            after = [ "network.target" ];
            description = "nara daemon ${name}";
            serviceConfig = {
              Type = "simple";
              User = "nara";
              ExecStart = "${cfg.package}/bin/nara -mqtt-host=${cfg.mqttHost}" 
                + (lib.optionalString (name != "") " -nara-id=${name}")
                + " " + (lib.concatStringsSep " " cfg.extraArgs);
              Restart = "always";
              RestartSec = 3;
              EnvironmentFile = lib.optional (cfg.environmentFile != null) cfg.environmentFile;
            };
          };
        };
      in lib.listToAttrs (map makeService instances);
  };
}
