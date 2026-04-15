package audit

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Result struct {
	HostName string
	HostAddr string
	ExitCode int
	Error    error
	Duration time.Duration
}

type LogEntry struct {
	CreatedAt  time.Time
	HostName   string
	HostAddr   string
	Command    string
	ExitCode   int
	DurationMS int64
	Error      string
	Operator   string
}

type Logger struct {
	db *sql.DB
}

func NewLogger(dbPath string) (*Logger, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	createSQL := `
CREATE TABLE IF NOT EXISTS audit_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME,
	host_name TEXT,
	host_addr TEXT,
	command TEXT,
	exit_code INTEGER,
	duration_ms INTEGER,
	error TEXT,
	operator TEXT
);`
	if _, err := db.Exec(createSQL); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Logger{db: db}, nil
}

func (l *Logger) Log(r *Result, command string) error {
	if l == nil || l.db == nil || r == nil {
		return fmt.Errorf("audit logger not initialized")
	}

	errText := ""
	if r.Error != nil {
		errText = r.Error.Error()
	}

	operator := os.Getenv("USERNAME")
	if operator == "" {
		operator = os.Getenv("USER")
	}

	_, err := l.db.Exec(
		`INSERT INTO audit_logs(created_at, host_name, host_addr, command, exit_code, duration_ms, error, operator)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now(), r.HostName, r.HostAddr, command, r.ExitCode, r.Duration.Milliseconds(), errText, operator,
	)
	return err
}

func (l *Logger) Query(limit int) ([]LogEntry, error) {
	if l == nil || l.db == nil {
		return nil, fmt.Errorf("audit logger not initialized")
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := l.db.Query(`
		SELECT created_at, host_name, host_addr, command, exit_code, duration_ms, error, operator
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]LogEntry, 0)
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.CreatedAt, &e.HostName, &e.HostAddr, &e.Command, &e.ExitCode, &e.DurationMS, &e.Error, &e.Operator); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (l *Logger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}
