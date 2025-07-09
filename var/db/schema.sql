CREATE TABLE IF NOT EXISTS tunnels (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  token TEXT NOT NULL,
  alias TEXT UNIQUE NOT NULL,
  port INTEGER UNIQUE NOT NULL,
  public_port INTEGER UNIQUE NOT NULL,
  type TEXT DEFAULT 'tcp' NOT NULL, -- 'tcp', 'http', 'https'
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  active BOOLEAN DEFAULT 0
);

CREATE TABLE IF NOT EXISTS reserved (
  alias_or_port TEXT PRIMARY KEY,
  type TEXT CHECK(type IN ('alias', 'port'))
);

CREATE TABLE IF NOT EXISTS user_stats (
  token TEXT PRIMARY KEY,
  last_creation_attempt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  creation_count INTEGER DEFAULT 0,
  max_tunnels INTEGER DEFAULT 5 -- Default limit of 5 active tunnels per user
);