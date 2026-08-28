// Package migrate runs database schema migrations by shelling out to the
// golang-migrate CLI, so the migrations directory uses its
// `NNNN_name.up.sql` / `NNNN_name.down.sql` layout.
package migrate

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sparkwing-dev/sparks-core/step"
)

type Config struct {
	// Dir and DSN are required.
	Dir string
	DSN string
	// Binary defaults to "migrate".
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

func args(c Config, sub ...string) []string {
	base := []string{"-source", "file://" + c.Dir, "-database", c.DSN}
	return append(base, sub...)
}

// Up applies all pending up migrations, and is a no-op at the latest version.
func Up(ctx context.Context, cfg Config) error {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	return step.Run(ctx, "migrate up", func(ctx context.Context) error {
		return step.Exec(ctx, cfg.Binary, args(cfg, "up")...)
	})
}

// Down rolls back exactly steps migrations, or every applied one when steps
// is not positive.
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

// Force clears the dirty flag golang-migrate sets when a migration fails
// partway, by declaring a version without touching the schema. Use it only
// after reconciling state by hand.
func Force(ctx context.Context, cfg Config, version int) error {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	return step.Run(ctx, "migrate force", func(ctx context.Context) error {
		return step.Exec(ctx, cfg.Binary, args(cfg, "force", strconv.Itoa(version))...)
	})
}
