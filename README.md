# nara

nara is an exploration on decentralized systems: it's an experimental network, a project without clear purpose, and a creative escape.

you can [see it live](https://nara.network) ([backup/debug site](https://global-nara.eljojo.net))

### what's a nara?

a nara is a single entity, it's stateful but it has no persistence. sometimes I think of it as a tamagotchi.

events are shared over [mqtt](https://en.wikipedia.org/wiki/MQTT) and nara observe them to form opinions. for example, by observing the network, nara can (independently) group in neighbourhoods based on their "vibes".

each nara has its own **personality** (Agreeableness, Sociability, and Chill), which dictates how it interacts with others, the trends it follows, and how it judges social interactions.

### Identity: The Soul

every nara has a **soul** - a portable cryptographic identity (~54 chars, Base58) that bonds a nara to its name.

- **Quirky Names**: unnamed naras get fun names like `stretchy-mushroom-421` (there are over 2.5 million possible name combinations!)
- **Gemstones** (💎, 🧿, 🏮): valid bond between soul and name
- **Shadows** (👤): invalid bond - soul was minted for a different name

souls are **portable** across machines. save your soul string to preserve identity:

```bash
# machine A
./nara -name jojo          # logs: 🔮 Soul: 5Kd3NBqT...

# machine B - same identity, different hardware
./nara -name jojo -soul 5Kd3NBqT...   # logs: 💎 Traveler
```

<details>
<summary>Technical details</summary>

A soul is 40 bytes: a 32-byte seed (HKDF from hardware fingerprint) + 8-byte HMAC tag proving the bond to a name. Validation recomputes the tag and checks it matches.

</details>

### Consensus: How Naras Agree

naras observe each other and must handle disagreements (clock drift, stale data, competing opinions).

**uptime-weighted clustering** resolves conflicts:
1. Cluster observations within 60-second tolerance
2. Pick winner via strategy hierarchy:
   - **Strong**: 2+ agreeing observers beats raw uptime
   - **Weak**: highest total uptime wins
   - **Coin flip** 🪙: if top 2 clusters are within 20%, flip a coin

longer-running naras are more credible - they've had time to converge on truth.

### Social Dynamics

naras don't just observe facts - they have **social interactions** that shape opinions over time.

#### Teasing

naras tease each other based on observed behavior:
- high restart count ("nice uptime, butterfingers")
- abandoning a trend everyone else follows
- coming back after being "missing"
- random playful jabs (personality-dependent)

teasing is **public** - the whole network sees it happen.

#### Clout and Reputation

each nara maintains **subjective opinions** about others. the same tease might be:
- hilarious to a high-sociability nara
- cringe to a high-chill nara
- offensive to a high-agreeableness nara

**clout** emerges from accumulated social interactions. but it's not global - every nara has their own view of who's cool and who's not.

```
same event → different observers → different opinions

raccoon sees lily tease bart: "haha, good one" → lily gains clout
zen-master sees lily tease bart: "unnecessary drama" → lily loses clout
```

#### Collective Memory

the network has **memory that survives individual restarts**:

- social events are stored in a local **ledger** (immutable facts)
- when a nara restarts, it requests memories from neighbors
- opinions are **derived** from the ledger, not stored directly
- same events + same personality = same opinions (deterministic)

this means the network "remembers" even if individual naras forget.

#### Event Sourcing

opinions are computed, not stored:

```
ledger (facts) → derivation function → opinions

opinion = f(events, my_soul, my_personality)
```

the derivation function is deterministic: replay the same events and you get the same opinions. but different naras with different personalities derive different opinions from the same events.

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
