-- 009: Single-live-generation invariant (Phase 3S correctness closure)
--
-- At most one accounting generation may exist in a live status (seeding,
-- materializing, active) at any time. The constant expression index key makes
-- the partial unique index apply one shared key across all three live
-- statuses. Failed and superseded generations are unlimited.
--
-- Migration 008 is already applied to existing (production) databases, so
-- this invariant is introduced additively here instead of amending 008.
-- Normal flows already uphold the invariant (seed creation checks for an
-- active generation and resumes interrupted seeds, and activation rechecks
-- inside the activation transaction). The index turns any future race between
-- concurrent runtimes into a clean constraint failure instead of two
-- interleaved generations writing derived state.

CREATE UNIQUE INDEX idx_accounting_generations_single_live
    ON accounting_generations ('live' || '')
    WHERE status IN ('seeding', 'materializing', 'active')
