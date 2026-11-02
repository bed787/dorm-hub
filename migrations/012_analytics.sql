CREATE TABLE analytics_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO analytics_settings(singleton) VALUES(true);
CREATE TABLE analytics_activity (
 day date NOT NULL,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 PRIMARY KEY(day,user_id)
);
CREATE INDEX analytics_activity_user ON analytics_activity(user_id);
CREATE TABLE analytics_listing_daily (
 day date NOT NULL,
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 listing_id bigint NOT NULL REFERENCES listings ON DELETE CASCADE,
 event text NOT NULL CHECK(event IN ('view','telegram','vk')),
 PRIMARY KEY(day,user_id,listing_id,event)
);
CREATE INDEX analytics_listing_daily_user ON analytics_listing_daily(user_id);
CREATE INDEX analytics_listing_daily_listing ON analytics_listing_daily(listing_id);
ALTER TABLE listings ADD COLUMN first_published_at timestamptz;
ALTER TABLE listings ADD COLUMN successful_at timestamptz;
-- Only reconstruct dates for which a record actually exists.
UPDATE listings l SET first_published_at=a.first_at FROM
 (SELECT target_id,min(created_at) AS first_at FROM moderation_actions
  WHERE target_type='listing' AND action='approve' GROUP BY target_id) a
 WHERE a.target_id=l.id;
UPDATE listings SET successful_at=completed_at
 WHERE completion_counted AND completion_reason='success';
CREATE INDEX listings_first_publication ON listings(first_published_at) WHERE first_published_at IS NOT NULL;
CREATE INDEX listings_successful ON listings(successful_at) WHERE successful_at IS NOT NULL;
CREATE FUNCTION remember_listing_milestones() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='active' AND NEW.first_published_at IS NULL THEN
  NEW.first_published_at=now();
 END IF;
 IF NEW.completion_counted AND NEW.successful_at IS NULL THEN
  NEW.successful_at=now();
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER listing_milestones BEFORE INSERT OR UPDATE OF status,completion_counted
 ON listings FOR EACH ROW EXECUTE FUNCTION remember_listing_milestones();
