-- No-op by design. Postgres cannot DROP a value from an enum type; reversing
-- would require recreating page_status and rewriting every dependent column,
-- which is not justified for this additive change. Rolling this migration back
-- leaves the two extra enum values in place (harmless — nothing is forced to
-- use them).
SELECT 1;
