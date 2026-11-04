package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buildoshi/e2b-api/db"
)

const fixturesDir = "fixtures"

// FixtureData is substituted into the fixture templates.
type FixtureData struct {
	// Issuer must equal the token issuer, so the demo user is the same row
	// that the first real login resolves to.
	Issuer string
}

// ApplyFixtures loads demo data in one transaction. Every statement is
// idempotent, so running it twice changes nothing.
func ApplyFixtures(ctx context.Context, pool *pgxpool.Pool, data FixtureData) (int, error) {
	files, err := fs.Glob(db.Fixtures, fixturesDir+"/*.sql")
	if err != nil {
		return 0, fmt.Errorf("list fixtures: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin fixtures transaction: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck // no-op after commit

	for _, file := range files {
		script, err := render(file, data)
		if err != nil {
			return 0, err
		}
		// The simple protocol allows several statements in one Exec.
		if _, err := tx.Exec(ctx, script, pgx.QueryExecModeSimpleProtocol); err != nil {
			return 0, fmt.Errorf("apply fixture %s: %w", file, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit fixtures: %w", err)
	}
	return len(files), nil
}

func render(file string, data FixtureData) (string, error) {
	raw, err := fs.ReadFile(db.Fixtures, file)
	if err != nil {
		return "", fmt.Errorf("read fixture %s: %w", file, err)
	}
	tmpl, err := template.New(file).Funcs(template.FuncMap{"quote": quote}).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse fixture %s: %w", file, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render fixture %s: %w", file, err)
	}
	return out.String(), nil
}

// quote renders s as a SQL string literal.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
