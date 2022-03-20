package delegate

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"sync"
	"time"
)

type Hashrates struct {
	mu sync.Mutex
	db *sql.DB
}

type Hashrate struct {
	Address    string
	MeasuredAt time.Time
	Hashrate   uint64
}

const file_hashrates string = "hashrates.db"

const create_hashrates string = `
  CREATE TABLE IF NOT EXISTS hashrates (
  	address TEXT NOT NULL,
    measured_at DATETIME NOT NULL,
    hashrate INTEGER NOT NULL
  );
  CREATE INDEX IF NOT EXISTS idx_address_date
    ON hashrates (address, measured_at);`

func NewHashrates() (*Hashrates, error) {
	db, err := sql.Open("sqlite3", file_hashrates)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(create_hashrates); err != nil {
		return nil, err
	}
	return &Hashrates{
		db: db,
	}, nil
}

func (c *Hashrates) Insert(hashrate Hashrate) error {
	_, err := c.db.Exec("INSERT INTO hashrates VALUES(?,?,?);", hashrate.Address, hashrate.MeasuredAt, hashrate.Hashrate)
	if err != nil {
		return err
	}

	return nil
}

func (c *Hashrates) Close() error {
	return c.db.Close()
}
