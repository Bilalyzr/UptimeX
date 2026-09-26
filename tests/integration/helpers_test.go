package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// adminCreateDatabase creates an empty database using a connection to the
// admin/database-creation database derived from the DSN.
func adminCreateDatabase(ctx context.Context, _ interface{ Ping(context.Context) error }, name string) error {
	db, err := sql.Open("pgx", withDBName(adminDSN()))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %s`, sqlIdent(name)))
	return err
}

func adminDropDatabase(ctx context.Context, _ interface{ Ping(context.Context) error }, name string) error {
	db, err := sql.Open("pgx", withDBName(adminDSN()))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, sqlIdent(name)))
	return err
}

func adminDSN() string {
	dsn := osGetenvDefault("TEST_POSTGRES_ADMIN_DSN", "")
	if dsn != "" {
		return dsn
	}
	return osGetenvDefault("TEST_POSTGRES_DSN", "")
}

// replaceDBName swaps the database component of a DSN.
func replaceDBName(dsn, newDB string) string {
	// DSN forms: URL (postgres://...) or key=value.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u := dsn
		if i := strings.LastIndex(u, "/"); i >= 0 {
			rest := u[i+1:]
			if j := strings.IndexAny(rest, "?"); j >= 0 {
				return u[:i+1] + newDB + rest[j:]
			}
			return u[:i+1] + newDB
		}
	}
	if i := strings.Index(dsn, "dbname="); i >= 0 {
		rest := dsn[i+len("dbname="):]
		end := strings.IndexAny(rest, " ")
		if end < 0 {
			end = len(rest)
		}
		return dsn[:i+len("dbname=")] + newDB + rest[end:]
	}
	return dsn + " dbname=" + newDB
}

func withDBName(dsn string) string { return replaceDBName(dsn, "postgres") }

func sqlIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func osGetenvDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
