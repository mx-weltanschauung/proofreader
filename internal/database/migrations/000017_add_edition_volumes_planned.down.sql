ALTER TABLE editions DROP CONSTRAINT IF EXISTS editions_volumes_planned_positive;
ALTER TABLE editions DROP COLUMN volumes_planned;
