-- Adds the location field the scorer's geo_impossible rule needs. Backfills
-- NULL for any rows already ingested before this ran — the rule treats a
-- missing city as "can't measure, don't guess" rather than flagging it.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS city TEXT;
