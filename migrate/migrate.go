// Package migrate runs database schema migrations as a sparkwing
// pipeline step. It drives golang-migrate by shelling out to the
// `migrate` CLI, so the migrations directory format is golang-migrate's
// (`NNNN_name.up.sql` / `NNNN_name.down.sql`).
//
// The `migrate` binary must be on PATH of the runner, alongside the
// other host tools sparks-core assumes (docker, kubectl, kind, git).
// Install: https://github.com/golang-migrate/migrate.
//
// Each function is shaped as func(ctx) error so it slots directly into
// a Job or Step body, typically between build and deploy:
//
//	sw.Job(plan, "migrate", func(ctx context.Context) error {
//	    dsn, err := sw.Secret(ctx, "DATABASE_URL")
//	    if err != nil {
//	        return err
//	    }
//	    return migrate.Up(ctx, migrate.Config{Dir: "db/migrations", DSN: dsn})
//	}).Needs(build)
package migrate

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sparkwing-dev/sparks-core/step"
)

// Config locates the migrations and the target database.
type Config struct {
	// Dir is the migrations directory in golang-migrate layout. Passed
	// to the CLI as a file:// source. Required.
	Dir string
	// DSN is the database connection string, e.g.
	// "postgres://user:pass@host:5432/db?sslmode=disable". Required.
	DSN string
	// Binary is the migrate CLI to invoke. Defaults to "migrate".
	Binary string
}

func (c *Config) defaults() {
	if c.Binary == "" {
		c.Binary = "migrate"
	}
}

func (c Config) validate() error {
	if c.Dir == "" {
		return fmt.Errorf("migrate: Dir is required")
	}
	if c.DSN == "" {
		return fmt.Errorf("migrate: DSN is required")
	}
	return nil
}

// args builds the migrate CLI argument vector: the source + database
// flags common to every subcommand, followed by the subcommand args.
func args(c Config, sub ...string) []string {
	base := []string{"-source", "file://" + c.Dir, "-database", c.DSN}
	return append(base, sub...)
}

// Up applies all pending up migrations. A no-op (no error) when the
// database is already at the latest version.
func Up(ctx context.Context, cfg Config) error {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	return step.Run(ctx, "migrate up", func(ctx context.Context) error {
		return step.Exec(ctx, cfg.Binary, args(cfg, "up")...)
	})
}

// Down rolls back migrations. steps > 0 rolls back exactly that many;
// steps <= 0 rolls back every applied migration. Use during recovery,
// not in the forward path of a deploy.
func Down(ctx context.Context, cfg Config, steps int) error {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	sub := []string{"down"}
	if steps > 0 {
		sub = append(sub, strconv.Itoa(steps))
	} else {
		sub = append(sub, "-all")
	}
	return step.Run(ctx, "migrate down", func(ctx context.Context) error {
		return step.Exec(ctx, cfg.Binary, args(cfg, sub...)...)
	})
}

// Force sets the migration version without running migrations, clearing
// the dirty flag golang-migrate sets when a migration fails partway.
// Recovery only: it tells the schema "you are at version N" without
// touching the schema, so use it after manually reconciling state.
func Force(ctx context.Context, cfg Config, version int) error {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	return step.Run(ctx, "migrate force", func(ctx context.Context) error {
		return step.Exec(ctx, cfg.Binary, args(cfg, "force", strconv.Itoa(version))...)
	})
}
