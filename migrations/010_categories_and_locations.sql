-- Retain listing IDs, photos, publication state and moderation history.
INSERT INTO categories(code,name) VALUES
 ('things','Техника'), ('clothes','Одежда и обувь'),
 ('accessories','Аксессуары'), ('home','Для дома'),
 ('study','Учёба'), ('help','Услуги'), ('other','Другое')
ON CONFLICT(code) DO UPDATE SET name=excluded.name;
ALTER TABLE listings ADD COLUMN category_needs_review boolean NOT NULL DEFAULT false;
-- Broad old categories require manual classification, not guesses from titles.
UPDATE listings SET category='other',category_needs_review=true,
 revision=revision+1 WHERE category IN ('things','housing');
DELETE FROM categories WHERE code='housing';

INSERT INTO locations(code,name) VALUES
 ('PETRODVORTSOVY','ПУНК'), ('KHALTURINA','Халтурина'), ('GSOM','ВШМ'),
 ('VASILEOSTROVSKY','Васильевский остров'), ('NEVSKY','Солидарности'),
 ('EXTERNAL','Другой адрес')
ON CONFLICT(code) DO UPDATE SET name=excluded.name;
UPDATE dorms SET location=CASE
 WHEN substring(name from '^№\s*([0-9]+)') IN ('8','9') THEN 'KHALTURINA'
 ELSE 'GSOM' END
WHERE location='PETRODVORTSOVY'
 AND substring(name from '^№\s*([0-9]+)') IN ('8','9','11');
UPDATE listings l SET location=d.location,revision=l.revision+1
FROM dorms d WHERE l.dorm_id=d.id AND l.location IS DISTINCT FROM d.location;
CREATE INDEX listings_category_review ON listings(id)
 WHERE category_needs_review AND deleted_at IS NULL;
