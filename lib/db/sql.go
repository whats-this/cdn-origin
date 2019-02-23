package db

var selectObjectByBucketKey = `
SELECT
	content_type,
	dest_url,
	"type"
FROM
	objects
WHERE
	bucket_key = $1
LIMIT 1
`
