-- "objects" table schema
CREATE TABLE IF NOT EXISTS objects (
  bucket_key VARCHAR(1088) NOT NULL UNIQUE, -- bucket + key (unique)
  bucket VARCHAR(20) NOT NULL, -- uint64 bucket ID ("public" for public bucket)
  "key" VARCHAR(1024) NOT NULL, -- Full bucket path to file (including directory)
  dir VARCHAR(1024) NOT NULL, -- Directory of file (with trailing slash)
  "type" integer NOT NULL DEFAULT 0, -- Object type enumerable (0 = file, 1 = short_url)
  backend_file_id VARCHAR(33) DEFAULT NULL, -- SeaweedFS file ID (only when object.type == 0)
  long_url VARCHAR(1024) DEFAULT NULL, -- Long URL (only when object.type == 1)
  content_type VARCHAR(255) DEFAULT 'application/octet-stream', -- Content-Type of file
  content_length INT DEFAULT NULL, -- Content-Length of file
  auth_hash VARCHAR(64) DEFAULT NULL, -- Authentication hash: sha256(user.id + password + object.id)
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- File creation timestamp
  md5_hash VARCHAR(32) DEFAULT NULL -- MD5 hash of file contents (or long URL)
);

-- Test file object: /index.md
INSERT INTO objects (bucket_key, bucket, key, dir, type, backend_file_id, content_type, content_length, md5_hash) VALUES (
  'public/index.txt',
  'public',
  '/index.txt',
  '/',
  0,
  '1,020c3fd6ab',
  'text/plain',
  0,
  'e2a81ac6617d7963bda5155239b4b262'
);

-- Test short_url object: /short_link
INSERT INTO objects (bucket_key, bucket, key, dir, type, long_url, content_type) VALUES (
  'public/short_path',
  'public',
  '/short_path',
  '/',
  1,
  'https://google.com',
  NULL
);
