---
title: Checkpoints
---

# Checkpoints

## Purpose

Checkpoints are multi-signature snapshots of historical uptime and restart data.
They anchor consensus when earlier events are missing or predate event sourcing, solving the "historian's note" problem.

## Conceptual Model

- A **Checkpoint** is a `SyncEvent` containing a snapshot of a nara's state at a specific time.
- It is built from multiple **Attestations** (votes) from peers who agree on the state.
- Once finalized, a checkpoint acts as a **Trusted Anchor**: subsequent state is derived by applying events on top of the checkpoint.

Key invariants:
- **Uniqueness**: Only one checkpoint per subject per `as_of_time` (enforced by ID).
- **Consensus**: Requires at least `MinCheckpointSignatures` (2) valid signatures to be trusted.
- **Permanence**: Finalized checkpoints are **Critical** events (priority 0) and are NEVER pruned from the ledger.
- **Backwards Compatibility**: Checkpoints bridge the gap between legacy tracking and event sourcing.

## External Behavior

- **Proposal**: Every 24 hours, a nara proposes a checkpoint about itself to the network.
- **Voting**: Peers receiving a proposal verify the data against their own view.
  - If they agree (within tolerance), they sign the proposal.
  - If they disagree, they reply with their own values.
- **Consensus**:
  - **Round 1**: Proposer collects matching signatures. If >= 5, it finalizes.
  - **Round 2**: If Round 1 fails, proposer calculates a **Trimmed Mean** of all votes (removing outliers), updates the values, and requests signatures again.
- **Publication**: The final multi-sig checkpoint is broadcast as a `SyncEvent` on `nara/checkpoint/final`.

## Interfaces

### MQTT Topics
- `nara/checkpoint/propose` -> `CheckpointProposal`
- `nara/checkpoint/vote` -> `CheckpointVote`
- `nara/checkpoint/final` -> `SyncEvent` (`svc: checkpoint`)

### CheckpointEventPayload (The Final Artifact)
Embedded in `SyncEvent`.
```json
{
  "subject": "nara-name",
  "subject_id": "nara-id",
  "as_of_time": 1700000000, // Unix seconds
  "observation": {
    "restarts": 42,
    "total_uptime": 123456,
    "start_time": 1690000000
  },
  "round": 2,
  "voter_ids": ["id1", "id2", ...],
  "signatures": ["sig1", "sig2", ...]
}
```

### Attestation (Signing Format)
The canonical string signed by voters:
`checkpoint:{subject_id}:{as_of_time}:{start_time}:{restarts}:{total_uptime}`

## Algorithms

### 1. Proposal Logic
- **Schedule**: Runs every 24h (checked every 15m).
- **Prerequisites**: Must have at least 6 online peers (Self + 5 voters) and be fully synced.
- **Values**: Uses locally derived state (`DeriveRestartCount`, `DeriveTotalUptime`).

### 2. Voting Logic (The Oracle)
When a peer receives a proposal:
- Compare proposed values to local view.
- **Tolerances**:
  - `Restarts`: ±5
  - `TotalUptime`: ±60 seconds
  - `StartTime`: ±60 seconds
- **Vote**:
  - If within tolerance -> Sign the *proposal's* values (Agree).
  - If outside tolerance -> Send *my* values (Disagree).

### 3. Consensus (Trimmed Mean)
Used in Round 2 if Round 1 fails.
- Collect all values from votes (agree + disagree).
- **Filter Outliers**: Remove values outside `[median * 0.2, median * 5.0]`.
- **Average**: Calculate mean of remaining values.
- **Result**: Use this average as the new proposal for Round 2.

### 4. Finalization
- Requires >= 5 signatures.
- Sort signatures by voter uptime (prefer high-uptime witnesses).
- Cap at 10 signatures to keep payload size manageable.

### 5. State Derivation (Using Checkpoints)
When calculating current state (`Restarts`, `Uptime`):
1. Find the latest **Checkpoint**.
   - `BaseRestarts = Checkpoint.Restarts`
   - `BaseUptime = Checkpoint.TotalUptime`
2. If no checkpoint, look for **Backfill** (migration event).
3. Replay **Events** since the Checkpoint/Backfill time:
   - `CurrentRestarts = BaseRestarts + Count(RestartEvents > AsOfTime)`
   - `CurrentUptime = BaseUptime + Sum(OnlinePeriods > AsOfTime)`

## Failure Modes

- **Consensus Failure**: If < 5 voters agree even after Round 2, no checkpoint is produced. Retries in 24h.
- **Verification Failure**: If signatures don't match public keys (e.g., key rotation), those signatures are ignored. Checkpoint remains valid if >= 2 valid signatures remain.
- **Cutoff Filtering**: To prevent bad historical data from a known bug, checkpoints before `CheckpointCutoffTime` (1768271051) are dropped on ingestion.

## Security / Trust Model

- **Multi-Sig**: No single node can fake historical truth; requires collusion of at least 2 (for acceptance) or 5 (for creation) peers.
- **Identity Binding**: Signatures are tied to `SubjectID` and `VoterID`, ensuring they apply to the correct naras.
- **Immutable Anchor**: Once created, a checkpoint is immutable. If a new one is created for the same time, the one with more signatures/uptime-weight wins (implicitly, though code currently treats them as first-come-first-served or duplicates).

## Test Oracle

- **Derivation**: `DeriveRestartCount` uses checkpoint + subsequent events. (`checkpoint_test.go`)
- **Voting**: Voters reject proposals outside tolerance. (`checkpoint_test.go`)
- **Signature Verification**: Invalid signatures are ignored; checkpoint requires threshold. (`checkpoint_signature_bug_test.go`)
- **Filtering**: Old checkpoints are dropped. (`sync_checkpoint_filter_test.go`)
- **Backfill Fallback**: Derivation falls back to backfill if no checkpoint exists. (`checkpoint_test.go`)

## Open Questions / TODO

- **Conflict Resolution**: If two valid checkpoints exist for the same time (fork), how to resolve? Currently likely undefined or "first wins".
- **Voter ID Consistency**: Ensure all producers populate `subject_id` correctly.
