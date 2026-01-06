# nara

nara is an exploration on decentralized systems: it's an experimental network, a project without clear purpose, and a creative escape.

you can [see it live](https://nara.network) ([backup/debug site](https://global-nara.eljojo.net))

### what's a nara?

a nara is a single entity, it's stateful but it has no persistence. sometimes I think of it as a tamagotchi.

events are shared over [mqtt](https://en.wikipedia.org/wiki/MQTT) and nara observe them to form opinions. for example, by observing the network, nara can (independently) group in neighbourhoods based on their "vibes".

each nara has its own **personality** (Agreeableness, Sociability, and Chill), which dictates how it interacts with others and the trends it follows.

### Identity and the Soul

every nara has a **soul** - a portable cryptographic identity that bonds a nara to its name. souls are deterministically generated from hardware fingerprints, but can be saved and moved between machines.

#### Soul Basics

- **Quirky Names**: if a nara is unnamed or has a generic hostname (like `raspberrypi`), it generates a fun, deterministic name like `stretchy-mushroom-421` or `cyber-star-099`. there are over 2.5 million possible name combinations!
- **The Soul**: a 54-character string (Base58 encoded) that cryptographically bonds to a name. on boot, every nara reveals its soul (e.g., `🔮 Soul: 5Kd3...x9Qm`).
- **The Gemstone**: a valid bond between soul and name is shown as a **Gemstone** (💎, 🧿, 🏮). this proves the soul was minted for this name.
- **The Shadow**: if a soul doesn't match its claimed name (a broken bond), it shows a shadow icon (👤) instead.

#### Portability: Travelers and Natives

souls are **portable**. you can move a nara's identity to different hardware:

```bash
# on machine A - note the soul
./nara -name jojo
# logs: 🔮 Soul: 5Kd3NBqT...

# on machine B - pass the soul to preserve identity
./nara -name jojo -soul 5Kd3NBqT...
# logs: ℹ️ Traveler: foreign soul (valid)
```

the soul remains **valid** on machine B because the cryptographic bond (soul ↔ name) is intact. it's just not **native** to that hardware.

| Scenario | Bond | Native | Status |
|----------|------|--------|--------|
| `./nara -name jojo` on HW1 | ✓ valid | ✓ native | 💎 |
| `./nara -name jojo -soul <HW1's soul>` on HW2 | ✓ valid | ✗ foreign | 💎 Traveler |
| `./nara -name jojo -soul <random>` | ✗ invalid | ✗ foreign | 👤 Warning |

#### Saving Your Soul

**important**: if you care about preserving a nara's identity, **save the soul string**.

souls are generated deterministically from hardware fingerprints (MAC addresses, host ID). if your hardware changes (new NIC, VM migration, docker rebuild), your nara will generate a *different* native soul and thus a different identity.

to preserve identity across hardware changes:

1. note the soul on first boot: `🔮 Soul: 5Kd3NBqT...`
2. save it somewhere (env var, secrets manager, config file)
3. pass it on subsequent boots: `-soul 5Kd3NBqT...` or `NARA_SOUL=5Kd3NBqT...`

for docker deployments, consider storing the soul in a volume or external config:

```yaml
environment:
  - NARA_SOUL=${SAVED_SOUL}  # from your secrets
```

#### How It Works (Technical)

a soul is 40 bytes encoded as Base58 (~54 chars):
- **seed** (32 bytes): derived via HKDF from hardware fingerprint
- **tag** (8 bytes): HMAC proving the bond to a specific name

```
tag = HMAC-SHA256(key=seed, msg="nara:name:v1:" + name)[:8]
soul = Base58(seed || tag)
```

validation is simple: recompute the tag from (seed, name) and check it matches.

#### The Flow of the Soul

```mermaid
graph TD
    Boot(Boot Sequence) --> CheckName{Is Name Generic?}
    CheckName -- Yes --> GenSeed[Generate Seed from Hardware]
    GenSeed --> GenName[Derive Name from Seed]
    GenName --> ComputeTag[Compute Bond Tag]
    ComputeTag --> Harmony(💎 Native Soul)

    CheckName -- No --> CheckSoul{Soul Provided?}
    CheckSoul -- No --> CustomSeed[Generate Seed from<br/>Hardware + Name]
    CustomSeed --> ComputeTag

    CheckSoul -- Yes --> ParseSoul[Parse Soul → seed, tag]
    ParseSoul --> ValidateBond{tag == HMAC(seed, name)?}
    ValidateBond -- No --> Shadow(👤 Invalid Bond<br/>Warning!)
    ValidateBond -- Yes --> CheckNative{Is Native Soul?}
    CheckNative -- Yes --> Harmony
    CheckNative -- No --> Traveler(💎 Valid Foreign Soul<br/>Traveler)
```

### Fashion and Trends

nara love to follow trends! they might start a new trend or join one started by their neighbors. 
- **Following the Wave**: a nara's personality determines how likely it is to join a trend or how quickly it might get bored and leave.
- **Visualizing Fashion**: on the web dashboard, you can see current trends listed. each trend is color-coded, making it easy to spot which naras are currently vibing together.

### Usage

- `--name`: The name for your nara. If generic or missing, a quirky name is generated from the soul.
- `--soul`: Provide a soul string to inherit an existing identity (Base58, ~54 chars).
- `--read-only`: Connect to the network but do not send any messages.
- `--serve-ui`: Serve the web UI at `/`.

#### Docker Compose

```yaml
services:
  nara:
    image: ghcr.io/eljojo/nara:latest
    restart: always
    environment:
      - NARA_ID=my-nara-instance
      - NARA_SOUL=  # optional: pass a saved soul to preserve identity
      - MQTT_HOST=tcp://your-mqtt-broker:1883
      - MQTT_USER=your_user
      - MQTT_PASS=your_password
    ports:
      - "8080:8080"
    command: ["-serve-ui"]
```

for fleets of containers with auto-generated names (no `NARA_ID`), each container gets a unique identity based on its MAC address. if you redeploy and want to keep the same identity, save and restore `NARA_SOUL`.

### NixOS Module

You can also use the provided NixOS module by adding it to your `imports`:

```nix
services.nara = {
  enable = true;
  mqttHost = "tcp://your-mqtt-broker:1883";
  instances = {
    lily = { };                          # generates soul from hardware
    rose = { soul = "5Kd3NBqT..."; };    # uses saved soul
  };
};
```

Each instance can have its own `soul` and `extraArgs`. Check `nara.nix` for more options.
