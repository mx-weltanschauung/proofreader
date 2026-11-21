CREATE OR REPLACE VIEW scans_hidden_works AS
    SELECT w.id FROM works w WHERE work_scans_hidden_by(w.id) <> '';

CREATE OR REPLACE VIEW closed_pages AS
    SELECT p.id, p.work_id, p.page_number
    FROM pages p
    WHERE p.work_id IN (SELECT id FROM closed_works)
       OR EXISTS (SELECT 1 FROM closed_chapters cc
                  WHERE cc.work_id = p.work_id
                    AND p.page_number BETWEEN cc.start_page AND cc.end_page);
