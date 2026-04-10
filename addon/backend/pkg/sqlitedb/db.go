package sqlitedb

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &DB{DB: db}, nil
}

func (db *DB) Run(ctx context.Context, probe func(error)) error {
	if err := db.Ping(); err != nil {
		probe(err)
		return fmt.Errorf("sqlite ping: %w", err)
	}
	probe(nil)
	<-ctx.Done()
	return db.Close()
}
