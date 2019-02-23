package db

import (
	"database/sql"

	// postgres driver for database/sql
	_ "github.com/lib/pq"
)

// DB is the current database connection.
var DB *sql.DB

// Connect to the database using the given driver and connection URL.
func Connect(driver string, connectionURL string) error {
	var err error
	DB, err = sql.Open(driver, connectionURL)
	if err != nil {
		return err
	}
	return DB.Ping()
}
