-- Раздел главной у собрания и у организации событий.
--
-- Главная раскладывает полки по именованным разделам; пусто — раздел по
-- умолчанию («Собрания сочинений» — имя печатает клиент). Имя раздела —
-- данные, а не код: новый раздел заводится вписанным в форму именем, и ни
-- одно имя конкретной читальни в платформу не попадает. Края обрезает
-- сервер (models.NormalizeShelfGroup), длину держит и он, и CHECK.
ALTER TABLE editions
    ADD COLUMN shelf_group text NOT NULL DEFAULT ''
        CONSTRAINT editions_shelf_group_length CHECK (char_length(shelf_group) <= 80);

ALTER TABLE event_organizations
    ADD COLUMN shelf_group text NOT NULL DEFAULT ''
        CONSTRAINT event_organizations_shelf_group_length CHECK (char_length(shelf_group) <= 80);
