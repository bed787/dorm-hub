INSERT INTO locations(code,name) VALUES ('EXTERNAL','Не УНК университета');
ALTER TABLE listings ADD COLUMN address text NOT NULL DEFAULT ''
 CHECK (char_length(address)<=300);
ALTER TABLE listings ADD CONSTRAINT listings_external_address
 CHECK (address='' OR location IS NOT DISTINCT FROM 'EXTERNAL');
ALTER TABLE listings ADD CONSTRAINT listings_external_no_dorm
 CHECK (location IS DISTINCT FROM 'EXTERNAL' OR dorm_id IS NULL);
