CREATE TABLE site_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    maintenance boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO site_settings(singleton) VALUES (true);
