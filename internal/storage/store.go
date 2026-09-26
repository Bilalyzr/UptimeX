package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	// PostgreSQL driver (production storage).
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	// Pure-Go SQLite driver (development / test storage, no cgo).
	_ "modernc.org/sqlite"

	"uptimex/migrations"
)

// Options configures the store. It mirrors config.DBConfig but keeps the
// storage package independent from configuration loading.
type Options struct {
	Driver          string // "postgres" | "sqlite"
	PostgresDSN     string
	SQLitePath      string // file path or ":memory:"
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// SQLStore implements Repository over database/sql for both drivers. The two
// dialects differ only in placeholder syntax and error shapes; the query
// bodies are shared.
type SQLStore struct {
	db      *sql.DB
	pg      bool // true when PostgreSQL
	memMode bool // true for SQLite :memory: (single connection)
}

// Open opens the database, applies pending migrations and returns a ready
// store. It fails fast on unreachable databases or failed migrations.
func Open(ctx context.Context, opts Options) (*SQLStore, error) {
	var (
		db  *sql.DB
		err error
		s   = &SQLStore{}
	)
	switch strings.ToLower(opts.Driver) {
	case "postgres", "pgx":
		s.pg = true
		db, err = sql.Open("pgx", opts.PostgresDSN)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
	case "sqlite":
		dsn := sqliteDSN(opts.SQLitePath)
		s.memMode = strings.Contains(opts.SQLitePath, ":memory:")
		db, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		if s.memMode {
			// Distinct connections would each get their own in-memory
			// database; force a single shared connection.
			db.SetMaxOpenConns(1)
		}
	default:
		return nil, fmt.Errorf("unsupported driver %q", opts.Driver)
	}

	maxOpen := opts.MaxOpenConns
	if s.memMode && (maxOpen <= 0 || maxOpen > 1) {
		maxOpen = 1
	}
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if opts.MaxIdleConns > 0 {
		db.SetMaxIdleConns(opts.MaxIdleConns)
	}
	if opts.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(opts.ConnMaxLifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s database: %w", opts.Driver, err)
	}
	s.db = db

	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func sqliteDSN(path string) string {
	// Pragmas: busy_timeout avoids spurious SQLITE_BUSY under concurrent
	// dev workloads; WAL allows readers during writes; foreign keys enforce
	// the same cascades PostgreSQL enforces.
	if strings.Contains(path, ":memory:") {
		return "file::memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
}

// Ping verifies the database is reachable.
func (s *SQLStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases the connection pool.
func (s *SQLStore) Close() error { return s.db.Close() }

// q rewrites ? placeholders to $n for PostgreSQL.
func (s *SQLStore) q(query string) string {
	if !s.pg {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString(fmt.Sprintf("$%d", n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isUniqueViolation reports whether err is a unique-constraint violation on
// either driver.
func (s *SQLStore) isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// migrate applies pending migrations for the active driver.
func (s *SQLStore) migrate(ctx context.Context) error {
	dir := "sqlite"
	if s.pg {
		dir = "postgres"
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at %s NOT NULL)`,
		timestampType(s.pg),
	)); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrations.FS.ReadDir(dir)
	if err != nil {
		return err
	}

	applied := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") || applied[name] {
			continue
		}
		body, err := migrations.FS.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(ctx, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) applyMigration(ctx context.Context, name, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`), name, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func timestampType(pg bool) string {
	if pg {
		return "TIMESTAMPTZ"
	}
	return "DATETIME"
}
