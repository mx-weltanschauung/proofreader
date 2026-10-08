-- Machine-proofreader statuses. Postgres 15: ADD VALUE is transaction-safe
-- (the PG<12 restriction does not apply), and neither value is USED in this
-- same transaction, so golang-migrate's wrapped run is fine.
ALTER TYPE page_status ADD VALUE IF NOT EXISTS 'вычитано_машиной';
ALTER TYPE page_status ADD VALUE IF NOT EXISTS 'требует_внимания';
