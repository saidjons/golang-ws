package database

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func InitDB() *sql.DB {
	var err error
	db, err := sql.Open("sqlite3", "./db.sqlite")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			room TEXT,
			username TEXT,
			content TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	return db
}
