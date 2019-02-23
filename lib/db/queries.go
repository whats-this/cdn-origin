package db

import (
	"database/sql"
	"fmt"
)

// SelectObjectByBucketKey returns an object from a bucket and a key.
func SelectObjectByBucketKey(bucket, key string) (Object, error) {
	var object Object

	var contentType sql.NullString
	var destURL sql.NullString
	var objectType int
	err := DB.QueryRow(selectObjectByBucketKey, fmt.Sprintf("%s/%s", bucket, key)).
		Scan(&contentType, &destURL, &objectType)
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
	object.ObjectType = objectType
	return object, nil
}
