package db

var selectObjectByBucketKey = `
SELECT
	content_type,
	dest_url,
	"type",
	deleted_at,
	delete_reason
FROM
	objects
WHERE
	bucket_key = $1
LIMIT 1
`
