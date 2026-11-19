CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notebooks (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (user_id, name COLLATE NOCASE)
);

CREATE UNIQUE INDEX IF NOT EXISTS notebooks_one_default
  ON notebooks(user_id) WHERE is_default = 1;

CREATE TABLE IF NOT EXISTS words (
  id               INTEGER PRIMARY KEY,
  notebook_id      INTEGER NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  word             TEXT NOT NULL,
  phonetic         TEXT NOT NULL DEFAULT '',
  translation      TEXT NOT NULL DEFAULT '',
  definition       TEXT NOT NULL DEFAULT '',
  examples_json    TEXT NOT NULL DEFAULT '[]',
  proficiency      INTEGER NOT NULL DEFAULT 5 CHECK (proficiency BETWEEN 1 AND 10),
  added_at         TEXT NOT NULL,
  last_reviewed_at TEXT,
  review_count     INTEGER NOT NULL DEFAULT 0,
  UNIQUE (notebook_id, word COLLATE NOCASE)
);

CREATE INDEX IF NOT EXISTS words_due ON words(notebook_id, last_reviewed_at);

CREATE TABLE IF NOT EXISTS recall_logs (
  id                INTEGER PRIMARY KEY,
  user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  notebook_id       INTEGER REFERENCES notebooks(id) ON DELETE SET NULL,
  notebook_name     TEXT NOT NULL,
  word_id           INTEGER REFERENCES words(id) ON DELETE SET NULL,
  word              TEXT NOT NULL,
  known             INTEGER NOT NULL,
  proficiency_after INTEGER NOT NULL,
  review_count      INTEGER NOT NULL,
  reviewed_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS recall_logs_user_time ON recall_logs(user_id, reviewed_at DESC);

CREATE TABLE IF NOT EXISTS ai_example_cache (
  word_lower TEXT PRIMARY KEY,
  payload    TEXT NOT NULL,
  model      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS word_groups (
  id             INTEGER PRIMARY KEY,
  notebook_id    INTEGER NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  last_viewed_at TEXT,
  UNIQUE (notebook_id, name COLLATE NOCASE)
);

CREATE INDEX IF NOT EXISTS word_groups_notebook
  ON word_groups(notebook_id, name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS word_group_members (
  group_id INTEGER NOT NULL REFERENCES word_groups(id) ON DELETE CASCADE,
  word_id  INTEGER NOT NULL REFERENCES words(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, word_id)
);

CREATE INDEX IF NOT EXISTS word_group_members_word
  ON word_group_members(word_id);

CREATE TABLE IF NOT EXISTS mcp_tokens (
  user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT
);
