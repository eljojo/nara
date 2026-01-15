# Checkpoints

## Purpose

Checkpoints are multi-signature snapshots of historical uptime and restart data.
They anchor consensus when earlier events are missing or predate event sourcing.

## Conceptual Model

- A checkpoint is a SyncEvent with a CheckpointEventPayload.
- It is built from many attestations (voter signatures) over the same values.

Key invariants:
- Each checkpoint is for one subject at one `as_of_time`.
- At least 2 valid signatures are required to trust a checkpoint.
- Finalized checkpoints are never pruned from the ledger.

## External Behavior

- Naras propose checkpoints about themselves every 24 hours.
- Proposals and votes flow over MQTT.
- Final checkpoints are broadcast for everyone to store.

## Interfaces

MQTT topics:
- `nara/checkpoint/propose` -> CheckpointProposal
- `nara/checkpoint/vote` -> CheckpointVote
- `nara/checkpoint/final` -> SyncEvent (ServiceCheckpoint)

CheckpointEventPayload fields:
- `subject`, `subject_id`
- `observation` (NaraObservation)
- `as_of_time` (seconds)
- `round` (1 or 2)
- `voter_ids[]`, `signatures[]`

Attestation signable content:
- `attestation:v{version}:{attester_id}:{subject_id}:{as_of_time}:{restarts}:{total_uptime}:{first_seen}`

## Event Types & Schemas (if relevant)

CheckpointProposal:
- Attestation (self-attestation) + `round`

CheckpointVote:
- Attestation (third-party) + `proposal_ts`, `round`, `approved`

## Algorithms

Proposal schedule:
- Every 24 hours (checked every 15 minutes).
- Requires at least 6 online naras (self + 5 voters).
- Skips until boot recovery completes.

Round 1:
- Proposer uses derived values from the ledger.
- Voters compare values to their own derived view with tolerances:
  - restarts: +/- 5
  - total_uptime: +/- 60 seconds
  - start_time: +/- 60 seconds
- Voters sign either the proposal values (approved) or their own values.

Round 2:
- If round 1 fails, proposer computes trimmed-mean values across votes and
  reproposes with `round=2`.
- Trimmed mean removes outliers outside [median*0.2, median*5.0].

Finalization:
- A consensus group requires at least 5 matching votes.
- Signatures are sorted by voter uptime and capped at 10.
- Proposer signature is included if it matches the final values.
- Final checkpoint is stored locally and published on `checkpoint/final`.

Acceptance:
- Checkpoints with fewer than 5 signatures are rejected on receipt.
- At least 2 signatures must verify against known public keys.
- Checkpoints at or before `CheckpointCutoffTime` are filtered on ingestion.

## Failure Modes

- Insufficient voters -> no checkpoint (wait 24h before retry).
- Unknown voter public keys -> fewer verifiable signatures.
- Old buggy checkpoints are discarded by cutoff filter.

## Security / Trust Model

- Each signature is an Ed25519 signature of a full attestation.
- Multi-signature requirement reduces single-node falsification.

## Test Oracle

- Checkpoint creation and validation. (`checkpoint_test.go`)
- Signature verification and filtering. (`checkpoint_signature_bug_test.go`, `checkpoint_vote_verification_bug_test.go`, `sync_checkpoint_filter_test.go`)

## Open Questions / TODO

- Use `subject_id` consistently in all producers (some legacy data omits it).
