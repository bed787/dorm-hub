CREATE TABLE users (
 id bigserial PRIMARY KEY,
 email text NOT NULL UNIQUE CHECK (email=lower(email)),
 nickname text NOT NULL,
 nickname_changed boolean NOT NULL DEFAULT false,
 telegram text NOT NULL DEFAULT '', vk text NOT NULL DEFAULT '',
 active boolean NOT NULL DEFAULT true, blocked boolean NOT NULL DEFAULT false,
 is_admin boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_nickname_unique ON users(lower(nickname));
CREATE TABLE login_codes (
 email text PRIMARY KEY, digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0)
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY, user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 csrf text NOT NULL, expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE locations(code text PRIMARY KEY, name text NOT NULL);
INSERT INTO locations VALUES ('PUNK','ПУНК'),('VUNK','ВУНК'),('DUNK','ДУНК');
CREATE TABLE dorms (
 id bigserial PRIMARY KEY, location text NOT NULL REFERENCES locations,
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100), UNIQUE(location,name)
);
CREATE TABLE categories(code text PRIMARY KEY, name text NOT NULL);
INSERT INTO categories VALUES
 ('things','Вещи и техника'),('study','Учёба'),('help','Услуги и помощь'),
 ('housing','Жильё и общежития'),('other','Другое');
CREATE TABLE listings (
 id bigserial PRIMARY KEY, author_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('offer','request')),
 category text NOT NULL REFERENCES categories,
 title text NOT NULL CHECK(char_length(title) BETWEEN 3 AND 120),
 description text NOT NULL CHECK(char_length(description) BETWEEN 10 AND 10000),
 location text NOT NULL REFERENCES locations,
 dorm_id bigint REFERENCES dorms ON DELETE SET NULL,
 price_mode text NOT NULL CHECK(price_mode IN ('price','exchange','unset')),
 price_minor bigint,
 telegram text NOT NULL DEFAULT '',vk text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','completed','archived')),
 completed_at timestamptz,
 hidden boolean NOT NULL DEFAULT false,deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 search_vector tsvector GENERATED ALWAYS AS
 (setweight(to_tsvector('russian',coalesce(title,'')),'A') || setweight(to_tsvector('russian',coalesce(description,'')),'B')) STORED,
 CHECK((price_mode='price' AND price_minor IS NOT NULL AND price_minor>=0 AND price_minor<=10000000000)
 OR (price_mode<>'price' AND price_minor IS NULL)),
 CHECK((status='completed' AND completed_at IS NOT NULL) OR status<>'completed'),
 CHECK(telegram<>'' OR vk<>'')
);
CREATE INDEX listings_feed ON listings(created_at DESC) WHERE deleted_at IS NULL AND NOT hidden;
CREATE INDEX listings_author ON listings(author_id,status);
CREATE INDEX listings_search ON listings USING gin(search_vector);
CREATE INDEX listings_filters ON listings(category,location,dorm_id);
CREATE TABLE photos (
 id text PRIMARY KEY,listing_id bigint NOT NULL REFERENCES listings ON DELETE CASCADE,
 position integer NOT NULL CHECK(position>=0),filename text NOT NULL UNIQUE,
 width integer NOT NULL CHECK(width>0),height integer NOT NULL CHECK(height>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(listing_id,position) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE favorites (
 user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 listing_id bigint NOT NULL REFERENCES listings ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,listing_id)
);
CREATE TABLE stars (
 from_user bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 to_user bigint NOT NULL REFERENCES users ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(from_user,to_user),CHECK(from_user<>to_user)
);
CREATE INDEX stars_target ON stars(to_user);
CREATE TABLE reports (
 id bigserial PRIMARY KEY,reporter_id bigint REFERENCES users ON DELETE SET NULL,
 listing_id bigint REFERENCES listings ON DELETE SET NULL,
 user_id bigint REFERENCES users ON DELETE SET NULL,
 target_type text NOT NULL CHECK(target_type IN ('listing','user')),
 target_id bigint NOT NULL,
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 10 AND 2000),
 status text NOT NULL DEFAULT 'new' CHECK(status IN ('new','resolved','dismissed')),
 resolution text NOT NULL DEFAULT '',resolved_by bigint REFERENCES users ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),resolved_at timestamptz
);
CREATE INDEX reports_queue ON reports(status,created_at DESC);
CREATE TABLE moderation_actions (
 id bigserial PRIMARY KEY,actor_id bigint REFERENCES users ON DELETE SET NULL,
 target_type text NOT NULL,target_id bigint NOT NULL,action text NOT NULL,reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
