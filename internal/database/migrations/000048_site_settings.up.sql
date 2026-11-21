CREATE TABLE site_settings (
    id integer PRIMARY KEY CHECK (id = 1),
    suggestions_per_hour integer NOT NULL CHECK (suggestions_per_hour > 0)
);

INSERT INTO site_settings (id, suggestions_per_hour) VALUES (1, 5);
