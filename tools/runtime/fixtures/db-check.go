package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

type CheckResult struct {
	QuickCheck     string `json:"quickCheck"`
	MigrationCount int    `json:"migrationCount"`
	SessionCount   int    `json:"sessionCount"`
	EventCount     int    `json:"eventCount"`
	TrafficCount   int    `json:"trafficCount"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: db-check <dbPath>\n")
		os.Exit(2)
	}
	dbPath := os.Args[1]
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var res CheckResult
	row := db.QueryRow("PRAGMA quick_check;")
	if err := row.Scan(&res.QuickCheck); err != nil {
		res.QuickCheck = fmt.Sprintf("error: %v", err)
	}

	_ = db.QueryRow("SELECT count(*) FROM schema_migrations;").Scan(&res.MigrationCount)
	_ = db.QueryRow("SELECT count(*) FROM collector_sessions;").Scan(&res.SessionCount)
	_ = db.QueryRow("SELECT count(*) FROM event_journal;").Scan(&res.EventCount)
	_ = db.QueryRow("SELECT count(*) FROM connection_traffic;").Scan(&res.TrafficCount)

	_ = json.NewEncoder(os.Stdout).Encode(res)
}
