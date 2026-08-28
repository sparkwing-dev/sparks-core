// Package services starts ephemeral backing services in Docker for
// integration tests: it runs a container, publishes its port to an ephemeral
// host port, waits for readiness, calls a caller-supplied function with the
// connection details, and force-removes the container afterward even when
// that function or the surrounding run fails.
package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

type Spec struct {
	// Image and ContainerPort are required.
	Image string
	// Name defaults to a unique generated name.
	Name string
	Env  map[string]string
	// ContainerPort is published to an ephemeral host port.
	ContainerPort int
	// Ready runs via `docker exec`; exit zero means ready, and an empty Ready
	// skips the wait.
	Ready []string
	// ReadyTimeout defaults to 30s.
	ReadyTimeout time.Duration
}

func (s *Spec) defaults() {
	if s.Name == "" {
		s.Name = fmt.Sprintf("sparks-svc-%d", time.Now().UnixNano())
	}
	if s.ReadyTimeout == 0 {
		s.ReadyTimeout = 30 * time.Second
	}
}

// With starts spec's container, waits for readiness, invokes fn with the
// ephemeral host port, and always force-removes the container.
func With(ctx context.Context, spec Spec, fn func(ctx context.Context, hostPort int) error) (err error) {
	spec.defaults()
	if spec.Image == "" {
		return fmt.Errorf("services: Image is required")
	}
	if spec.ContainerPort <= 0 {
		return fmt.Errorf("services: ContainerPort is required")
	}

	return step.Run(ctx, "service ("+spec.Image+")", func(ctx context.Context) error {
		runArgs := []string{"run", "-d", "--name", spec.Name}
		for k, v := range spec.Env {
			runArgs = append(runArgs, "-e", k+"="+v)
		}
		runArgs = append(runArgs, "-p", fmt.Sprintf("127.0.0.1::%d", spec.ContainerPort), spec.Image)
		if err := step.Exec(ctx, "docker", runArgs...); err != nil {
			return err
		}
		// safety: teardown must run even when the run ctx is cancelled, or the container leaks.
		defer func() {
			cleanupCtx := context.WithoutCancel(ctx)
			if rmErr := step.Exec(cleanupCtx, "docker", "rm", "-f", spec.Name); rmErr != nil {
				sparkwing.Warn(ctx, "services: failed to remove %s: %v", spec.Name, rmErr)
			}
		}()

		hostPort, err := hostPortFor(ctx, spec.Name, spec.ContainerPort)
		if err != nil {
			return err
		}
		sparkwing.Info(ctx, "%s ready on localhost:%d", spec.Image, hostPort)

		if err := waitReady(ctx, spec); err != nil {
			return err
		}
		return fn(ctx, hostPort)
	})
}

func hostPortFor(ctx context.Context, name string, containerPort int) (int, error) {
	out, err := sparkwing.Exec(ctx, "docker", "port", name, strconv.Itoa(containerPort)+"/tcp").String()
	if err != nil {
		return 0, fmt.Errorf("services: docker port %s: %w", name, err)
	}
	return parseHostPort(out)
}

func parseHostPort(out string) (int, error) {
	line := strings.TrimSpace(out)
	if line == "" {
		return 0, fmt.Errorf("services: empty docker port output (container not publishing the port?)")
	}
	line = strings.SplitN(line, "\n", 2)[0]
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return 0, fmt.Errorf("services: cannot parse host port from %q", line)
	}
	port, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
	if err != nil {
		return 0, fmt.Errorf("services: cannot parse host port from %q: %w", line, err)
	}
	return port, nil
}

func waitReady(ctx context.Context, spec Spec) error {
	if len(spec.Ready) == 0 {
		return nil
	}
	deadline := time.Now().Add(spec.ReadyTimeout)
	execArgs := append([]string{"exec", spec.Name}, spec.Ready...)
	for {
		if _, err := sparkwing.Exec(ctx, "docker", execArgs...).Capture(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("services: %s not ready after %s", spec.Image, spec.ReadyTimeout)
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type Postgres struct {
	// Image defaults to "postgres:16-alpine".
	Image string
	// User, Password, and DB each default to "postgres".
	User     string
	Password string
	DB       string
	// Name defaults to a unique generated name.
	Name string
	// ReadyTimeout defaults to 30s.
	ReadyTimeout time.Duration
}

// WithPostgres is [With] for an ephemeral Postgres, invoking fn with a
// ready-to-use DSN.
func WithPostgres(ctx context.Context, cfg Postgres, fn func(ctx context.Context, dsn string) error) error {
	if cfg.Image == "" {
		cfg.Image = "postgres:16-alpine"
	}
	if cfg.User == "" {
		cfg.User = "postgres"
	}
	if cfg.Password == "" {
		cfg.Password = "postgres"
	}
	if cfg.DB == "" {
		cfg.DB = "postgres"
	}
	spec := Spec{
		Image: cfg.Image,
		Name:  cfg.Name,
		Env: map[string]string{
			"POSTGRES_USER":     cfg.User,
			"POSTGRES_PASSWORD": cfg.Password,
			"POSTGRES_DB":       cfg.DB,
		},
		ContainerPort: 5432,
		Ready:         []string{"pg_isready", "-U", cfg.User, "-d", cfg.DB},
		ReadyTimeout:  cfg.ReadyTimeout,
	}
	return With(ctx, spec, func(ctx context.Context, hostPort int) error {
		return fn(ctx, PostgresDSN(cfg.User, cfg.Password, hostPort, cfg.DB))
	})
}

// PostgresDSN builds a libpq connection string for localhost:port.
func PostgresDSN(user, password string, port int, db string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable", user, password, port, db)
}
