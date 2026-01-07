# nara

nara is a framework for **distributed agents with collective memory**.

it's a simulation where autonomous agents observe events, form opinions based on personality, and interact with each other. the network is a **collective hazy memory** - no single nara has the complete picture, but together they remember.

you can [see it live](https://nara.network) ([backup/debug site](https://global-nara.eljojo.net))

## how it works

```
┌───────────────────┬─────────────────────────────────────┐
│    PUBLIC INFO    │           EVENT STORE               │
│    (Newspaper)    │       (Collective Memory)           │
├───────────────────┼─────────────────────────────────────┤
│ Authority: ME     │ Authority: NOBODY                   │
│ Audience: ALL     │ Audience: WHOEVER HEARD IT          │
├───────────────────┼─────────────────────────────────────┤
│ - my coordinates  │ - "A teased B"                      │
│ - my flair/buzz   │ - "A completed a journey"           │
│ - my public key   │ - "A came online"                   │
└───────────────────┴─────────────────────────────────────┘
```

**public info** is authoritative - you define your own status and broadcast it.
**event store** is hearsay - things that happened, spread through gossip.

events spread through two channels:
- **plaza** (MQTT broadcast) - the public square where everyone hears announcements
- **zines** (P2P gossip) - events passed hand-to-hand between neighbors via mesh

naras can use MQTT-only (traditional), gossip-only (P2P), or hybrid (both). apps stay transport-agnostic - they don't know or care how events arrive. when a nara boots, it catches up by asking neighbors what it missed, then spreads and receives news organically.

```
ledger (facts) → derivation function → opinions

opinion = f(events, my_soul, my_personality)
```

the derivation is deterministic: same events + same personality = same opinions.

---

## what's a nara?

a nara is a single entity, stateful but with no persistence. think of it as a tamagotchi.

each nara has its own **personality** (Agreeableness, Sociability, and Chill), which dictates how it interacts with others, the trends it follows, and how it judges social interactions.

## identity: the soul

every nara has a **soul** - a portable cryptographic identity (~54 chars, Base58) that bonds a nara to its name.

- **Quirky Names**: unnamed naras get fun names like `stretchy-mushroom-421` (over 2.5 million combinations!)
- **Gemstones** (💎, 🧿, 🏮): valid bond between soul and name
- **Shadows** (👤): invalid bond - soul was minted for a different name

souls are **portable** across machines:

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

## social dynamics

naras don't just observe facts - they have **social interactions** that shape opinions over time.

### teasing

naras tease each other based on observed behavior:
- high restart count ("nice uptime, butterfingers")
- abandoning a trend everyone else follows
- coming back after being "missing"
- random playful jabs (personality-dependent)

teasing is **public** - the whole network sees it happen.

### clout and reputation

each nara maintains **subjective opinions** about others. the same tease might be:
- hilarious to a high-sociability nara
- cringe to a high-chill nara
- offensive to a high-agreeableness nara

**clout** emerges from accumulated social interactions. but it's not global - every nara has their own view of who's cool.

```
same event → different observers → different opinions

raccoon sees lily tease bart: "haha, good one" → lily gains clout
zen-master sees lily tease bart: "unnecessary drama" → lily loses clout
```

### collective memory

the network **remembers** even when individual naras forget:

- events are stored in a local **ledger** (immutable facts)
- when a nara restarts, it requests memories from neighbors
- opinions are **derived** from events, not stored directly

## fashion and trends

naras love to follow trends! they might start a new trend or join one started by their neighbors.

- **Following the Wave**: personality determines how likely a nara is to join a trend or get bored and leave
- **Visualizing Fashion**: on the web dashboard, trends are color-coded so you can spot which naras are vibing together

## network architecture & scalability

nara uses a hybrid MQTT + mesh architecture designed to scale from 5 to 5000 nodes:

- **MQTT Plaza** (public square): lightweight announcements, social events, journey completions
- **Mesh HTTP** (direct connections): event syncing, ping measurements
- **Event-Driven Observations**: distributed consensus on network state (restarts, uptime, online status)

### the newspaper problem

originally, naras broadcast their entire state every 5-55 seconds via "newspapers" containing observations about all known naras. at scale:
- **5000 nodes**: 750KB-1MB per broadcast = 68MB/s - 1GB/s network traffic
- **doesn't scale** past ~100 nodes

### event-driven solution

instead of broadcasting complete state, naras emit lightweight events when changes occur:
- `restart` events: when a nara restarts
- `first-seen` events: first time observing a nara
- `status-change` events: online/offline transitions

**benefits:**
- **99.99% traffic reduction**: 83 KB/s at 5000 nodes (vs 68MB/s - 1GB/s)
- **eventual consistency**: events spread organically through gossip
- **anti-abuse protection**: 4-layer defense against malicious nodes
- **graceful migration**: backfill mechanism preserves historical knowledge

**background sync:** naras perform lightweight periodic syncing (~30 min intervals) to catch up on critical events missed due to personality-based filtering, helping the collective memory stay strong.

for details, see [OBSERVATIONS.md](OBSERVATIONS.md).

---

## usage

- `--name`: The name for your nara. If generic or missing, a quirky name is generated from the soul.
- `--soul`: Provide a soul string to inherit an existing identity (Base58, ~54 chars).
- `--read-only`: Connect to the network but do not send any messages.
- `--serve-ui`: Serve the web UI at `/`.

### docker compose

```yaml
services:
  nara:
    image: ghcr.io/eljojo/nara:latest
    restart: always
    environment:
      - NARA_ID=my-nara-instance # optional: give your nara a name
      - NARA_SOUL=  # optional: pass a saved soul to preserve identity
    ports:
      - "8080:8080"
    command: ["-serve-ui"]
```

for fleets of containers with auto-generated names (no `NARA_ID`), each container gets a unique identity based on its MAC address. if you redeploy and want to keep the same identity, save and restore `NARA_SOUL`.

### NixOS module

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
