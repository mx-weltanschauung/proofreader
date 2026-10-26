-- Имя раздела теряется; вернуть его — вписать заново в форме собрания и
-- организации. Ничего, кроме раскладки главной, от поля не зависит.
ALTER TABLE event_organizations DROP COLUMN shelf_group;
ALTER TABLE editions DROP COLUMN shelf_group;
