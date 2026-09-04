ALTER TABLE plugin_installations ADD COLUMN source TEXT NOT NULL DEFAULT 'official'
    CHECK (source IN ('official', 'developer'));

ALTER TABLE plugin_installations ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'tuf_verified'
    CHECK (verification_status IN ('tuf_verified', 'developer_unverified'));

ALTER TABLE plugin_installations ADD COLUMN source_conflict INTEGER NOT NULL DEFAULT 0
    CHECK (source_conflict IN (0, 1));
