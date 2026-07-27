package usagestats

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultUsageTable = "usage_statistics"
	defaultUsageKey   = "default"
)

type postgresSnapshotStorage struct {
	db    *sql.DB
	table string
}

// ConfigurePostgres replaces the process-wide usage store with a PostgreSQL-backed store.
func ConfigurePostgres(ctx context.Context, dsn, schema string, enabled bool) (*Store, error) {
	store, errStore := NewPostgresStore(ctx, dsn, schema, enabled)
	if errStore != nil {
		return nil, errStore
	}
	configureStore(store)
	return store, nil
}

// NewPostgresStore loads usage aggregates from PostgreSQL and starts the asynchronous flush worker.
func NewPostgresStore(ctx context.Context, dsn, schema string, enabled bool) (*Store, error) {
	storage, errStorage := newPostgresSnapshotStorage(ctx, dsn, schema)
	if errStorage != nil {
		return nil, errStorage
	}
	return newStore(ctx, storage, enabled)
}

func newPostgresSnapshotStorage(ctx context.Context, dsn, schema string) (*postgresSnapshotStorage, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("usage statistics postgres store: DSN is required")
	}
	db, errOpen := sql.Open("pgx", dsn)
	if errOpen != nil {
		return nil, fmt.Errorf("usage statistics postgres store: open database: %w", errOpen)
	}
	if errPing := db.PingContext(ctx); errPing != nil {
		_ = db.Close()
		return nil, fmt.Errorf("usage statistics postgres store: ping database: %w", errPing)
	}

	schema = strings.TrimSpace(schema)
	if schema != "" {
		query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quotePostgresIdentifier(schema))
		if _, errCreateSchema := db.ExecContext(ctx, query); errCreateSchema != nil {
			_ = db.Close()
			return nil, fmt.Errorf("usage statistics postgres store: create schema: %w", errCreateSchema)
		}
	}
	table := quotePostgresIdentifier(defaultUsageTable)
	if schema != "" {
		table = quotePostgresIdentifier(schema) + "." + table
	}
	if _, errCreateTable := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			content JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, table)); errCreateTable != nil {
		_ = db.Close()
		return nil, fmt.Errorf("usage statistics postgres store: create table: %w", errCreateTable)
	}
	return &postgresSnapshotStorage{db: db, table: table}, nil
}

func (s *postgresSnapshotStorage) Load(ctx context.Context) ([]byte, error) {
	query := fmt.Sprintf("SELECT content::text FROM %s WHERE id = $1", s.table)
	var content string
	errQuery := s.db.QueryRowContext(ctx, query, defaultUsageKey).Scan(&content)
	if errors.Is(errQuery, sql.ErrNoRows) {
		return nil, nil
	}
	if errQuery != nil {
		return nil, fmt.Errorf("load snapshot: %w", errQuery)
	}
	return []byte(content), nil
}

func (s *postgresSnapshotStorage) Save(ctx context.Context, data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("save snapshot: invalid JSON")
	}
	query := fmt.Sprintf(`
		INSERT INTO %s (id, content, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (id)
		DO UPDATE SET content = EXCLUDED.content, updated_at = NOW()
	`, s.table)
	if _, errExec := s.db.ExecContext(ctx, query, defaultUsageKey, json.RawMessage(data)); errExec != nil {
		return fmt.Errorf("save snapshot: %w", errExec)
	}
	return nil
}

func (s *postgresSnapshotStorage) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
