-- Replace campus abbreviations without deleting existing listings or dorms.
INSERT INTO locations(code,name) VALUES
 ('VASILEOSTROVSKY','Василеостровский район'),
 ('PETRODVORTSOVY','Петродворцовый район'),
 ('NEVSKY','Невский район'),
 ('UNSPECIFIED','Район нужно уточнить')
ON CONFLICT(code) DO UPDATE SET name=excluded.name;

-- PUNK and VUNK have known geographical equivalents. The former DUNK value
-- does not identify a district in the supplied specification: retain it as
-- unspecified rather than assigning old data to an arbitrary district.
UPDATE dorms SET location=CASE location
 WHEN 'PUNK' THEN 'PETRODVORTSOVY'
 WHEN 'VUNK' THEN 'VASILEOSTROVSKY'
 ELSE 'UNSPECIFIED' END
WHERE location IN ('PUNK','VUNK','DUNK');
UPDATE listings SET location=CASE location
 WHEN 'PUNK' THEN 'PETRODVORTSOVY'
 WHEN 'VUNK' THEN 'VASILEOSTROVSKY'
 ELSE 'UNSPECIFIED' END
WHERE location IN ('PUNK','VUNK','DUNK');
DELETE FROM locations WHERE code IN ('PUNK','VUNK','DUNK');

-- The supplied screenshot groups several buildings together. Preserve only
-- the address detail that it explicitly assigns to each group of dorms.
INSERT INTO dorms(location,name) VALUES
 ('VASILEOSTROVSKY','№ 1 — ул. Кораблестроителей, 20'),
 ('VASILEOSTROVSKY','№ 2 — ул. Кораблестроителей, 20'),
 ('VASILEOSTROVSKY','№ 3 — ул. Кораблестроителей, 20'),
 ('VASILEOSTROVSKY','№ 4 — ул. Шевченко, 25'),
 ('VASILEOSTROVSKY','№ 5 — ул. Шевченко, 25'),
 ('VASILEOSTROVSKY','№ 17 — 5-я линия В. О., 66'),
 ('VASILEOSTROVSKY','№ 18 — 8-я линия В. О., 66'),
 ('VASILEOSTROVSKY','№ 19 — ул. Капитанская, 3'),
 ('PETRODVORTSOVY','№ 8 — ул. Халтурина, 15'),
 ('PETRODVORTSOVY','№ 9 — ул. Халтурина, 15'),
 ('PETRODVORTSOVY','№ 10 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 11 — Михайловская дача'),
 ('PETRODVORTSOVY','№ 12 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 13 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 14 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 15 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 16 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 20 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 21 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 22 — ул. Ботаническая'),
 ('PETRODVORTSOVY','№ 23 — ул. Ботаническая'),
 ('NEVSKY','№ 6 — пр. Солидарности, 27/1')
ON CONFLICT(location,name) DO NOTHING;
