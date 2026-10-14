ALTER TABLE site_settings
    ADD COLUMN maintenance_reason text NOT NULL DEFAULT ''
    CHECK (char_length(maintenance_reason) <= 1000);

-- Preserve a currently enabled maintenance mode when updating the application.
UPDATE site_settings
SET maintenance_reason='Сайт временно закрыт. Пожалуйста, загляните позже.'
WHERE maintenance;
