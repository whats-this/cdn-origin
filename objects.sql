-- "objects" table schema
CREATE TABLE IF NOT EXISTS objects (
  id VARCHAR(20) PRIMARY KEY NOT NULL, -- uint64 Twitter snowflake ID (primary)
  bucket VARCHAR(20) NOT NULL, -- uint64 bucket ID (0 = public)
  "key" VARCHAR(1024) NOT NULL, -- Full bucket path to file (including directory)
  bucket_key VARCHAR(1088) NOT NULL UNIQUE, -- bucket + key (unique)
  "type" integer NOT NULL DEFAULT 0, -- Object type enumerable (0 = file, 1 = short_url)
  backend_file_id VARCHAR(33) DEFAULT NULL, -- SeaweedFS file ID (only when object.type == 0)
  long_url VARCHAR(1024) DEFAULT NULL, -- Long URL (only when object.type == 1)
  content_type VARCHAR(255) DEFAULT 'application/octet-stream', -- Content-Type of file
  auth_hash VARCHAR(64) DEFAULT NULL, -- Authentication hash: sha256(user.id + password + object.id)
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- File creation timestamp
  md5_hash VARCHAR(32) DEFAULT NULL -- MD5 hash of file contents (or long URL)
);

-- Test file object: /index.md
INSERT INTO objects (id, bucket, key, bucket_key, type, backend_file_id, content_type, md5_hash) VALUES (
  '0',
  '0',
  '/index.txt',
  '0/index.txt',
  0,
  '1,020c3fd6ab',
  'text/plain',
  'e2a81ac6617d7963bda5155239b4b262'
);

-- Test short_url object: /short_link
INSERT INTO objects (id, bucket, key, bucket_key, type, long_url, content_type, md5_hash) VALUES (
  '1',
  '0',
  '/short_path',
  '0/short_path',
  1,
  'https://google.com',
  NULL,
  '99999ebcfdb78df077ad2727fd00969f'
);
