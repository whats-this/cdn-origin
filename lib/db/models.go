package db

// Object represents a partial object from the database.
type Object struct {
	ContentType *string `json:"content_type"`
	DestURL     *string `json:"dest_url"`
	ObjectType  int     `json:"object_type"`
}
