package storage

import (
	"database/sql"
	"time"
)

// nullTime scans a nullable timestamp column on both drivers.
type nullTime struct {
	sql.NullTime
}

func (n *nullTime) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

// nullBool scans a nullable boolean column (PostgreSQL BOOLEAN or SQLite 0/1).
type nullBool struct {
	sql.NullBool
}

// nullStr scans a nullable string column.
type nullStr struct {
	sql.NullString
}

func (n *nullStr) Ptr() *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// timeArg converts a time for binding; zero times become NULL.
func timeArg(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// timePtrArg binds *time.Time (nil becomes NULL).
func timePtrArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// strPtrArg binds *string (nil becomes NULL).
func strPtrArg(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
