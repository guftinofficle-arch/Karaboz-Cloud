CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS developer_users (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), email TEXT NOT NULL UNIQUE,
 display_name TEXT NOT NULL, password_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS projects (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES developer_users(id) ON DELETE CASCADE,
 name TEXT NOT NULL, storage_limit_bytes BIGINT NOT NULL DEFAULT 1073741824 CHECK (storage_limit_bytes > 0),
 bucket_limit INTEGER NOT NULL DEFAULT 10 CHECK (bucket_limit > 0), created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS projects_user_id_idx ON projects(user_id);
CREATE TABLE IF NOT EXISTS api_keys (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 name TEXT NOT NULL, key_prefix TEXT NOT NULL, key_hash TEXT NOT NULL UNIQUE,
 permission TEXT NOT NULL DEFAULT 'read_write' CHECK (permission IN ('read_write','read_only')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), last_used_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS api_keys_project_id_idx ON api_keys(project_id);
CREATE TABLE IF NOT EXISTS buckets (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 name TEXT NOT NULL, storage_name TEXT NOT NULL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(project_id,name)
);
CREATE INDEX IF NOT EXISTS buckets_project_id_idx ON buckets(project_id);
CREATE TABLE IF NOT EXISTS objects (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), bucket_id UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
 object_key TEXT NOT NULL, size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
 content_type TEXT NOT NULL DEFAULT 'application/octet-stream', etag TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(bucket_id,object_key)
);
CREATE INDEX IF NOT EXISTS objects_bucket_updated_idx ON objects(bucket_id,updated_at DESC);
CREATE TABLE IF NOT EXISTS audit_events (
 id BIGSERIAL PRIMARY KEY, project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
 event_type TEXT NOT NULL, details JSONB NOT NULL DEFAULT '{}'::jsonb, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_project_created_idx ON audit_events(project_id,created_at DESC);
