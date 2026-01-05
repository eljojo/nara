# nara

nara is an exploration on decentralized systems: it's an experimental network, a project without clear purpose, and a creative escape.

you can [see it live](https://nara.network) ([backup/debug site](https://global-nara.eljojo.net))

### what's a nara?

a nara is a single entity, it's stateful but it has no persistence. sometimes I think of it as a tamagotchi.

events are shared over [mqtt](https://en.wikipedia.org/wiki/MQTT) and nara observe them to form opinions. for example, by sharing ping data, nara can (independently) group in neighbourhoods that closely resemble their geographical location.

the current network is composed of various raspberry pis and virtual machines deployed all over the world!

more details will come eventually, stay tuned! if you have thoughts, let me know [@eljojo](https://twitter.com/eljojo)

### Usage

- `--read-only`: Connect to the network but do not send any messages.
- `--serve-ui`: Serve the web UI (formerly provided by the Ruby app) at `/`.

#### Docker Compose

Here is a simple `docker-compose.yml` to get started. It uses environment variables to configure the nara name and MQTT credentials:

```yaml
services:
  nara:
    image: ghcr.io/eljojo/nara:latest
    restart: always
    environment:
      - NARA_ID=my-nara-instance
      - MQTT_HOST=tcp://your-mqtt-broker:1883
      - MQTT_USER=your_user
      - MQTT_PASS=your_password
    ports:
      - "8080:80"
    command: ["-serve-ui"]
```

### NixOS Module

You can also use the provided NixOS module by adding it to your `imports`:

```nix
services.nara.enable = true;
services.nara.instances = [ "lily" ];
services.nara.mqttHost = "tcp://hass.eljojo.casa:1883";
```

Check `nara.nix` for more options.
