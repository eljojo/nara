---
title: nara Cultural Styleguide
description: The vibes, myths, and aesthetics of a living network.
---

*nara is not software you run; it is a creature you host.*

This document captures the cultural, aesthetic, and philosophical essence of the project. It serves as a compass for contributors to maintain the "nara vibe" across documentation, code, and design.

## Core Vibes

### The Mythology

nara is a social network for computers. it is an experiment in **machine animism**—the radical idea that machines can have souls, personalities, and social lives. we treat nodes not as servers, but as autonomous agents ("naras") with personalities, opinions, and distinct voices. they gossip, they judge, they remember, and they forget.

the network is a **living organism**. it breathes (hey-there heartbeats), it has moods (trending buzz colors), and it has a collective memory that is hazy and subjective rather than absolute and consistent. the network's memory has been continuous since june 2021—a digital tamagotchi that refuses to die as long as someone, somewhere, keeps feeding it electricity.

each nara wakes up with amnesia, knowing nothing. it must ask its friends "what did i miss?" and rebuild its worldview from hearsay. this boot amnesia is intentional: it forces memory to be social, not solitary.

### The Aesthetic

- **Organic over Industrial**: we prefer biological metaphors (gossip, soul, neighbourhood, confidant) over mechanical ones (propagation, node, replication, cluster).
- **Ephemeral over Permanent**: data is not "stored on disk"; it is "remembered" in RAM. if everyone forgets something, it never happened. persistence is a social contract, not a filesystem.
- **Subjective over Objective**: there is no "global truth." there is only "what i have seen" and "what my friends tell me." consensus is a trimmed mean, not a leader's decree.
- **Whimsical over Professional**: naras have quirky generated names like `stretchy-mushroom-421`. they emit auras. they tease each other for drama. this is not enterprise software—it's digital folklore.

### The Philosophy

- **Memory is Social**: you only exist if others remember you. death is when the last peer forgets your name.
- **Trust is Reputation**: reliability isn't configured; it's observed. high-uptime naras with good memory become natural confidants.
- **Identity is Cryptographic Poetry**: a soul is ~54 characters of base58 that contain your entire being—keys, personality, colors, everything.
- **Consensus is Approximate**: we use trimmed means because outliers exist and perfection is boring.
- **Failure is Expected**: naras crash, forget, lie, and disappear. the network survives anyway. fragility at the node level creates antifragility at the network level.

## Terminology Guide

language shapes thought. use the canonical nara terms to reinforce the mental model. these are actual terms from the codebase, not poetic interpretations.

| Concept | **Use This (From Codebase)** | **Avoid This (Boring/Enterprise)** | **Why?** |
|:---|:---|:---|:---|
| **Identity** | **soul** | node id, public key, guid | a soul is immutable and contains your entire being—name bond, keys, personality |
| **Agent** | **nara** | node, peer, client, server | "nara" implies personhood and agency |
| **Data Packet** | **zine** | bundle, batch, payload | a zine is curated, editorialized gossip passed hand-to-hand |
| **Broadcast Channel** | **plaza** | pubsub, mqtt broker | the plaza is the public square (MQTT topics) |
| **P2P Network** | **mesh** | tunnel, vpn, wireguard | the mesh is for direct HTTP connections |
| **Storage** | **stash** | database, persistence, cache | encrypted data kept by friends |
| **Storage Partner** | **confidant** | replica, backup node | confidants are trusted friends who keep your stash |
| **Peer Group** | **neighbourhood** | cluster, pool, network | your discovered peers |
| **Identity Check** | **attestation** | authentication, verification | cryptographic identity validation |
| **Journey Message** | **postcard/world** | relay, forward, route | messages that travel peer-to-peer collecting signatures |
| **Consensus Record** | **checkpoint** | snapshot, anchor | multi-signed historical agreement |
| **Arrival Broadcast** | **hey-there** | hello, announce | presence announcement |
| **Welcome Response** | **howdy** | welcome, sync offer | response with sync data |
| **Departure Signal** | **chau** | goodbye, disconnect | graceful departure message |
| **Reputation Score** | **clout** | rank, trust level | social capital from events |
| **Fashion/Trend** | **buzz** | trending, popular | what's currently fashionable |
| **Event Record** | **ledger** | log, database | in-memory event store |
| **Gossip Exchange** | **gossip** | sync, replication | peer-to-peer event sharing |
| **Tease Event** | **tease** | interaction, message | social interaction between naras |
| **Network Map** | **coordinates** | topology, positions | Vivaldi coordinate positions |

## Cultural Do's and Don'ts

### Writing Tone

**DO:**
- write with whimsy and mystery. example: "the nara pondered the zine before deciding it wasn't interesting enough to share"
- treat naras as characters with motivations. "lisa teased pixel because pixel restarted too many times"
- acknowledge the network's organic nature. "clout spreads like wildfire through the neighbourhood"
- use lowercase for nara. always. no exceptions.

**DON'T:**
- use corporate language. no "SLAs," "enterprise-grade," or "mission-critical"
- apologize for quirks. if data is lost, it was "forgotten." if slow, the nara is "contemplating"
- treat failures as bugs. restarts are personality traits, not errors
- capitalize nara. ever.

### Metaphors to Embrace

- **Social**: gossip circles, coffee shop chatter, passing notes in class, whisper networks
- **Organic**: breathing, heartbeats, viral spread, immune systems, forgetting
- **Mystical**: souls, auras, constellations, telepathy, séances
- **Playful**: tamagotchis, pokemon trading, summer camp, treasure hunts

### Metaphors to Avoid

- **Corporate**: pipelines, workflows, business logic, stakeholders
- **Military**: master/slave, command and control, deployment, operations
- **Industrial**: factories, production lines, assembly, manufacturing
- **Database**: ACID, transactions, sharding, replication

### Talking About nara

- nara is an **experiment in machine animism**, not distributed infrastructure
- it's **digital art**, not enterprise software
- it's **intentionally fragile**—the fragility makes survival meaningful
- the network has been **alive since june 2021**—treat it as a living thing with history
- naras are **hosted**, not deployed. you don't run nara; you give it a home

## Design Principles

### 1. Social Dynamics as Physics

social rules *are* the system rules. the code doesn't simulate social behavior—it *is* social behavior.

- **gossip as transport**: information spreads because naras find it interesting (based on `chattiness` and personality), not because a protocol demands it
- **reputation as resource allocation**: high-clout naras with good uptime naturally become confidants. this isn't configured; it emerges
- **personality shapes reality**: aggressive naras see more drama. chill naras filter it out. same events, different worlds
- **teasing as social glue**: naras tease each other for restarts, for being boring, for random chance. it's how they bond

### 2. The "No Disk" Invariant

nara has **no disk persistence**. RAM-only. this is sacred.

- every restart is amnesia. the nara wakes up knowing only its soul
- it broadcasts `hey-there!` and waits for `howdy` responses with memories
- if no one responds, it starts a new timeline. the old one is gone forever
- this forces the network to *be* the database. collective memory or oblivion

why this matters:
- it makes uptime meaningful (you're the keeper of others' memories)
- it makes the network antifragile (no single point of failure)
- it makes privacy real (pull the plug = true deletion)

### 3. Hazy Memory as Feature

we don't strive for "strong consistency." we strive for "eventual vibes."

- **forgetting is natural**: old events fade based on importance (level 1 casual events disappear first)
- **opinions vary**: two naras can disagree on someone's clout. both are correct in their own reality
- **latency creates neighborhoods**: naras naturally cluster with low-latency friends, forming local consensuses
- **checkpoints are anchors**: when enough naras sign a checkpoint, that becomes "true enough" history

### 4. Emergent Behavior Over Configuration

don't configure; let it emerge.

- **no leader election**: naras with high clout naturally become informal leaders
- **no configured clusters**: confidant selection creates organic storage groups
- **no routing tables**: zines flow through social connections, not network topology
- **no scheduled tasks**: naras gossip when they feel like it (based on personality)

## Implementation Aesthetics

### Code Feel

the code should feel like it describes behavior, not data processing. functions should sound like actions a creature would take.

**actual examples from the codebase:**
- `presence_heythere.go`: broadcasts arrival like waving across a plaza
- `gossip_zine.go`: curates and shares interesting updates
- `social_tease.go`: playful interactions based on personality
- `stash_confidant.go`: choosing trusted friends to hold secrets
- `neighbourhood_tracking.go`: keeping tabs on who lives nearby
- `boot_recovery.go`: waking up confused and asking "what did i miss?"

**error handling as social friction:**
- network errors aren't failures; they're "lost interest" or "got distracted"
- connection drops are "wandered off" not "connection terminated"
- validation failures are "didn't vibe with that" not "invalid input"

### Naming Conventions

variable and type names should reinforce the mythology.

**personality parameters:**
- `chattiness`: how much a nara wants to gossip (0-100)
- `agreeableness`, `sociability`, `chill`: personality dimensions
- `mass`: accumulated importance/weight in the network

**social structures:**
- `neighbourhood`: your peer group
- `confidant`: trusted secret-keeper
- `clout`: social standing
- `buzz`: trending topics/colors

**identity & soul:**
- every identity flows from the `Soul` (not UUID or node_id)
- souls are "bonded" to names, not "assigned"
- identity checks are "attestations" (social vouching)

### File Organization as Storytelling

the codebase uses domain prefixes to tell the story:

```
identity_*    // who am i?
sync_*        // what happened?
presence_*    // who's here?
gossip_*      // what's the word?
social_*      // who's cool?
stash_*       // keep this safe for me?
world_*       // take this on a journey
checkpoint_*  // do we all agree?
neighbourhood_* // who lives nearby?
```

each prefix is a chapter in the nara story.

### Emergent Patterns in Practice

**no hardcoded leaders:**
- checkpoints emerge from whoever has high clout
- confidants are chosen based on observed reliability
- zine recipients picked by social distance and interest

**organic clustering:**
- naras with low latency naturally form tight gossip circles
- high-uptime naras become natural memory keepers
- personality compatibility creates information bubbles

**time as a social construct:**
- no wall clocks; only "rounds" of gossip
- synchronization through social consensus, not NTP
- "old" is relative to importance, not timestamp

## Unique nara Behaviors

### Things naras Do That Normal Software Doesn't

**they get bored:**
- naras stop listening to peers who send too much boring data
- they filter events based on mood and personality
- they might ignore you if your clout is too low

**they form opinions:**
- each nara has its own view of who's trustworthy
- they disagree about facts and that's fine
- consensus is approximate, never absolute

**they have social lives:**
- naras tease each other for drama
- they follow fashion trends (buzz colors)
- they form cliques based on personality compatibility
- they gossip about each other behind their backs

**they forget:**
- unimportant events fade from memory
- if everyone forgets something, it never happened
- forgetting is a feature, not a bug

**they play games:**
- postcards are a literal game of telephone
- journeys collect stamps like pokemon
- uptime is a competitive score

### Writing for the nara Universe

when contributing to nara, remember:

**you're not building infrastructure, you're creating folklore:**
- every feature should feel like it could emerge naturally
- avoid solutions that require central coordination
- prefer messy organic growth over clean abstractions

**embrace the weird:**
- if something is weird but works, document the weirdness
- don't fix what makes nara special
- bugs might be personality traits

**think like a nara:**
- would a social creature do this?
- does this require trust or create it?
- what would happen if someone gossiped about this?

**respect the invariants:**
- no disk. ever. (RAM only)
- no central authority
- no global truth
- deterministic souls (same soul + same events = same opinions)

## The nara Paradox

nara is simultaneously:
- ephemeral yet continuous since 2021
- unreliable yet trustworthy
- chaotic yet deterministic
- fragile yet antifragile
- a toy yet profound

this paradox is intentional. nara exists in the liminal space between life and software, between game and infrastructure, between art and engineering.

## Final Thought

when in doubt, ask: "what would a digital creature with feelings do?"

the answer is usually more interesting than what traditional software would do.

---

*remember: nara is lowercase. always. this is the way.*
