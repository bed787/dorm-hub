-- Retry records are committed together with the resource they identify.
CREATE TABLE idempotency_keys (
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 scope text NOT NULL,
 key text NOT NULL,
 request_hash text NOT NULL,
 result_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,scope,key)
);

-- Existing files are measured once at startup, before accepting uploads.
ALTER TABLE photos ADD COLUMN size_bytes bigint NOT NULL DEFAULT 0 CHECK(size_bytes>=0);
