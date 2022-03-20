package delegate

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"sync"
	"time"
)

type Solutions struct {
	mu sync.Mutex
	db *sql.DB
}

type Solution struct {
	Address  string
	SolvedAt time.Time
	Hash     string
}

const file_solutions string = "solutions.db"

const create_solutions string = `
  CREATE TABLE IF NOT EXISTS solutions (
  	address TEXT NOT NULL,
    solved_at DATETIME NOT NULL,
    hash TEXT NOT NULL
  );
  CREATE INDEX IF NOT EXISTS idx_address
    ON solutions (address);`

func NewSolutions() (*Solutions, error) {
	db, err := sql.Open("sqlite3", file_solutions)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(create_solutions); err != nil {
		return nil, err
	}
	return &Solutions{
		db: db,
	}, nil
}

func (c *Solutions) Insert(solution Solution) error {
	_, err := c.db.Exec("INSERT INTO solutions VALUES(?,?,?);", solution.Address, solution.SolvedAt, solution.Hash)
	if err != nil {
		return err
	}

	return nil
}

func (c *Solutions) FindAll() ([]Solution, error) {
	result, err := c.db.Query("SELECT address, solved_at, hash From solutions;")
	if err != nil {
		return []Solution{}, err
	}
	defer result.Close()

	var solutions []Solution

	for result.Next() {
		solution := Solution{}
		err := result.Scan(&solution.Address, &solution.SolvedAt, &solution.Hash)
		if err != nil {
			return []Solution{}, err
		}
		solutions = append(solutions, solution)
	}

	return solutions, nil
}

func (c *Solutions) Close() error {
	return c.db.Close()
}
