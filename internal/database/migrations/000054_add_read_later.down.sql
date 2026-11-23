DO $$
BEGIN
    LOCK TABLE read_later IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM read_later) THEN
        RAISE EXCEPTION 'read_later contains saved publications';
    END IF;
END $$;

DROP TABLE read_later;
