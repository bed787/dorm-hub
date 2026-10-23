-- A durable, coalescing queue. No FK: deleted listings must still be removed
-- from the external index. Generations prevent acknowledging a later edit.
CREATE SEQUENCE search_index_version;
CREATE TABLE search_index_queue (
 listing_id bigint PRIMARY KEY,
 version bigint NOT NULL DEFAULT nextval('search_index_version')
);
CREATE FUNCTION enqueue_listing_search() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO search_index_queue(listing_id)
 VALUES(CASE WHEN TG_OP='DELETE' THEN OLD.id ELSE NEW.id END)
 ON CONFLICT(listing_id) DO UPDATE SET version=nextval('search_index_version');
 RETURN NULL;
END $$;
CREATE TRIGGER listings_search_queue AFTER INSERT OR UPDATE OR DELETE ON listings
 FOR EACH ROW EXECUTE FUNCTION enqueue_listing_search();

CREATE FUNCTION enqueue_author_search() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO search_index_queue(listing_id)
 SELECT id FROM listings WHERE author_id=NEW.id
 ON CONFLICT(listing_id) DO UPDATE SET version=nextval('search_index_version');
 RETURN NULL;
END $$;
CREATE TRIGGER users_search_queue AFTER UPDATE OF active,blocked ON users
 FOR EACH ROW WHEN (OLD.active IS DISTINCT FROM NEW.active OR OLD.blocked IS DISTINCT FROM NEW.blocked)
 EXECUTE FUNCTION enqueue_author_search();
INSERT INTO search_index_queue(listing_id) SELECT id FROM listings;
