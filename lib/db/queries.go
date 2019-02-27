package db

import (
	"database/sql"
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
	err := DB.QueryRow(selectObjectByBucketKey, fmt.Sprintf("%s/%s", bucket, key)).
		Scan(&contentType, &destURL, &objectType, &deletedAt, &deleteReason)
	if err != nil {
		return object, err
	}

	// Populate object values
	if contentType.Valid {
		object.ContentType = &contentType.String
	}
	if destURL.Valid {
		object.DestURL= &destURL.String
	}
	if deletedAt.Valid {
		object.DeletedAt = &deletedAt.Time
		if deleteReason.Valid {
			object.DeleteReason = &deleteReason.String
		}
	}
	object.ObjectType = objectType
	return object, nil
}
