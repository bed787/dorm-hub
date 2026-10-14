-- Drafts are private and may contain incomplete fields. Publication is validated
-- by the service; the database also protects the pending/published invariant.
ALTER TABLE listings DROP CONSTRAINT listings_status_check;
ALTER TABLE listings DROP CONSTRAINT listings_title_check;
ALTER TABLE listings DROP CONSTRAINT listings_description_check;
ALTER TABLE listings DROP CONSTRAINT listings_check2;
ALTER TABLE listings ALTER COLUMN category DROP NOT NULL;
ALTER TABLE listings ALTER COLUMN location DROP NOT NULL;
ALTER TABLE listings ALTER COLUMN status SET DEFAULT 'archived';
ALTER TABLE listings ADD CONSTRAINT listings_status_check CHECK(status IN ('active','pending','archived','completed'));
ALTER TABLE listings ADD CONSTRAINT listings_title_check CHECK(char_length(title)<=120);
ALTER TABLE listings ADD CONSTRAINT listings_description_check CHECK(char_length(description)<=10000);
ALTER TABLE listings ADD CONSTRAINT listings_publication_check CHECK(
 status NOT IN ('active','pending') OR
 (char_length(title)>=3 AND char_length(description)>=10 AND category IS NOT NULL AND location IS NOT NULL AND (telegram<>'' OR vk<>''))
);
ALTER TABLE listings ADD COLUMN archive_kind text NOT NULL DEFAULT '' CHECK(archive_kind IN ('','draft','rejected','held'));
ALTER TABLE listings ADD COLUMN moderation_comment text NOT NULL DEFAULT '';
ALTER TABLE listings ADD COLUMN revision bigint NOT NULL DEFAULT 1;
ALTER TABLE listings ADD COLUMN completion_counted boolean NOT NULL DEFAULT false;
ALTER TABLE listings ADD COLUMN completion_reason text NOT NULL DEFAULT '' CHECK(completion_reason IN ('','success','no_longer_needed'));
-- This archive has not launched publicly: existing local active entries must
-- pass the same review as new entries.
UPDATE listings SET status='pending' WHERE status='active';
UPDATE listings SET archive_kind='held' WHERE status='archived';
CREATE INDEX listings_review_queue ON listings(updated_at,id) WHERE status='pending' AND deleted_at IS NULL;

-- Aggregate only: deleting accounts must not decrement or expose past deals.
CREATE TABLE site_statistics(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),completed_deals bigint NOT NULL DEFAULT 0 CHECK(completed_deals>=0));
INSERT INTO site_statistics(singleton) VALUES(true);
CREATE TABLE account_deletion_codes(
 user_id bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
 session_hash text NOT NULL,
 digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0)
);
-- Durable file cleanup after the database commits; no account identifiers.
CREATE TABLE pending_file_deletions(filename text PRIMARY KEY,created_at timestamptz NOT NULL DEFAULT now());
