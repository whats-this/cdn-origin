package db

import (
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/lib/pq"
)

// SelectObjectByBucketKey returns an object from a bucket and a key.
func SelectObjectByBucketKey(bucket, key string) (Object, error) {
	var object Object

	var contentType sql.NullString
	var destURL sql.NullString
	var objectType int
	var deletedAt pq.NullTime
	var deleteReason sql.NullString
	var md5Hash []byte
	var sha256Hash []byte
	err := DB.QueryRow(selectObjectByBucketKey, fmt.Sprintf("%s/%s", bucket, key)).
		Scan(&contentType, &destURL, &objectType, &deletedAt, &deleteReason, &md5Hash, &sha256Hash)
	if err != nil {
		return object, err
	}

	// Populate object values
	if contentType.Valid {
		object.ContentType = &contentType.String
	}
	if destURL.Valid {
		object.DestURL = &destURL.String
	}
	if deletedAt.Valid {
		object.DeletedAt = &deletedAt.Time
		if deleteReason.Valid {
			object.DeleteReason = &deleteReason.String
		}
	}
	if md5Hash != nil && len(md5Hash) == 16 {
		object.MD5HashBytes = md5Hash
		md5String := hex.EncodeToString(md5Hash)
		object.MD5Hash = &md5String
	}
	if sha256Hash != nil && len(sha256Hash) == 32 {
		object.SHA256HashBytes = sha256Hash
		sha256String := hex.EncodeToString(sha256Hash)
		object.SHA256Hash = &sha256String
	}
	object.ObjectType = objectType
	return object, nil
}
