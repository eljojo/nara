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
      type = lib.types.attrsOf (lib.types.submodule {
        options = {
          soul = lib.mkOption {
            type = lib.types.nullOr lib.types.str;
            default = null;
            description = "The soul for this instance. Save this to preserve identity across hardware changes.";
          };
          extraArgs = lib.mkOption {
            type = lib.types.listOf lib.types.str;
            default = [ ];
            description = "Extra arguments for this instance.";
          };
        };
      });
      default = { };
      description = "Nara instances to run. Keys are instance names.";
      example = lib.literalExpression ''
        {
          lily = { soul = "5Kd3NBqT..."; };
          rose = { };  # will generate new soul from hardware
        }
      '';
    };

    mqttHost = lib.mkOption {
      type = lib.types.str;
      default = "tcp://hass.eljojo.casa:1883";
      description = "MQTT broker address.";
    };

    extraArgs = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Extra arguments to pass to all nara instances.";
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
        makeService = name: instanceCfg: {
          name = "nara-${name}";
          value = {
            wantedBy = [ "multi-user.target" ];
            after = [ "network.target" ];
            description = "nara daemon ${name}";
            serviceConfig = {
              Type = "simple";
              User = "nara";
              ExecStart = "${cfg.package}/bin/nara -mqtt-host=${cfg.mqttHost}"
                + " -nara-id=${name}"
                + (lib.optionalString (instanceCfg.soul != null) " -soul=${instanceCfg.soul}")
                + " " + (lib.concatStringsSep " " (cfg.extraArgs ++ instanceCfg.extraArgs));
              Restart = "always";
              RestartSec = 3;
              EnvironmentFile = lib.optional (cfg.environmentFile != null) cfg.environmentFile;
            };
          };
        };
      in lib.mapAttrs' makeService cfg.instances;
  };
}
