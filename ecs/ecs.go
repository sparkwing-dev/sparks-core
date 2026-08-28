// Package ecs rolls an ECS/Fargate service to a new image by re-registering
// its task definition, and rolls it back to the prior revision Deploy
// returns. It shells out to the `aws` CLI and honors SPARKWING_DRY_RUN by
// echoing argv, reads included, so a dry run needs no AWS credentials.
//
// A re-registered revision does not carry the running task definition's
// tags: describe reads only the taskDefinition body, so cost-allocation or
// ownership tags must be reapplied out of band.
package ecs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

const stablePollInterval = 15 * time.Second

const dryRunEnv = "SPARKWING_DRY_RUN"

// registerReadOnlyKeys are fields describe-task-definition returns that
// register-task-definition rejects on input.
var registerReadOnlyKeys = []string{
	"taskDefinitionArn",
	"revision",
	"status",
	"requiresAttributes",
	"compatibilities",
	"registeredAt",
	"registeredBy",
	"deregisteredAt",
}

type DeployConfig struct {
	// Cluster, Service, TaskFamily, ContainerName, and Image are required.
	Cluster       string
	Service       string
	TaskFamily    string
	ContainerName string
	Image         string
	// Region empty omits --region and lets the aws CLI resolve it.
	Region string
	// AWSProfile empty resolves via AWS_PROFILE, or is dropped under IRSA.
	AWSProfile        string
	RegisterArgs      []string
	UpdateServiceArgs []string
	// Timeout zero uses the aws CLI's built-in `wait services-stable`
	// waiter, whose cap is fixed at roughly ten minutes; a non-zero value
	// polls describe-services instead, allowing shorter and longer waits.
	Timeout time.Duration
	// DryRun echoes the aws argv without executing, as SPARKWING_DRY_RUN does.
	DryRun bool
}

func (c DeployConfig) validate() error {
	missing := make([]string, 0, 5)
	if c.Cluster == "" {
		missing = append(missing, "Cluster")
	}
	if c.Service == "" {
		missing = append(missing, "Service")
	}
	if c.TaskFamily == "" {
		missing = append(missing, "TaskFamily")
	}
	if c.ContainerName == "" {
		missing = append(missing, "ContainerName")
	}
	if c.Image == "" {
		missing = append(missing, "Image")
	}
	if len(missing) > 0 {
		return fmt.Errorf("ecs.Deploy: %s required", strings.Join(missing, ", "))
	}
	return nil
}

func (c DeployConfig) dryRun() bool {
	return c.DryRun || os.Getenv(dryRunEnv) != ""
}

// Deploy rolls a service to a new image and waits for it to stabilize,
// returning the prior task-definition ARN for Rollback. That ARN is empty
// under dry-run.
func Deploy(ctx context.Context, cfg DeployConfig) (prevTaskDef string, err error) {
	if verr := cfg.validate(); verr != nil {
		return "", verr
	}
	dry := cfg.dryRun()
	err = step.Run(ctx, "ecs deploy", func(ctx context.Context) error {
		rp := regionProfileArgs(cfg.Region, cfg.AWSProfile)
		descArgs := describeArgs(cfg.TaskFamily, rp)
		if dry {
			echoArgv(ctx, descArgs)
			echoArgv(ctx, registerArgs(dryRunInputRef, cfg.RegisterArgs, rp))
			echoArgv(ctx, updateServiceArgs(cfg.Cluster, cfg.Service, dryRunTaskDefRef, cfg.UpdateServiceArgs, rp))
			echoArgv(ctx, waitArgv(cfg, rp))
			return nil
		}
		descJSON, derr := sparkwing.Exec(ctx, "aws", descArgs...).String()
		if derr != nil {
			return fmt.Errorf("ecs: describe task definition %s: %w", cfg.TaskFamily, derr)
		}
		input, prior, berr := buildRegisterInput([]byte(descJSON), cfg.ContainerName, cfg.Image)
		if berr != nil {
			return berr
		}
		prevTaskDef = prior
		path, terr := writeTaskDefFile(input)
		if terr != nil {
			return terr
		}
		defer func() { _ = os.Remove(path) }()
		newArn, rerr := sparkwing.Exec(ctx, "aws", registerArgs("file://"+path, cfg.RegisterArgs, rp)...).String()
		if rerr != nil {
			return fmt.Errorf("ecs: register task definition %s: %w", cfg.TaskFamily, rerr)
		}
		sparkwing.Info(ctx, "registered %s (prior %s)", newArn, prior)
		if _, uerr := sparkwing.Exec(ctx, "aws", updateServiceArgs(cfg.Cluster, cfg.Service, newArn, cfg.UpdateServiceArgs, rp)...).Run(); uerr != nil {
			return fmt.Errorf("ecs: update service %s: %w", cfg.Service, uerr)
		}
		sparkwing.Info(ctx, "waiting for service %s to stabilize", cfg.Service)
		return waitForStable(ctx, cfg, rp)
	})
	return prevTaskDef, err
}

func waitArgv(cfg DeployConfig, rp []string) []string {
	if cfg.Timeout > 0 {
		return describeServicesArgs(cfg.Cluster, cfg.Service, rp)
	}
	return waitStableArgs(cfg.Cluster, cfg.Service, rp)
}

func waitForStable(ctx context.Context, cfg DeployConfig, rp []string) error {
	if cfg.Timeout <= 0 {
		if _, err := sparkwing.Exec(ctx, "aws", waitStableArgs(cfg.Cluster, cfg.Service, rp)...).Run(); err != nil {
			return fmt.Errorf("ecs: wait for service %s to stabilize: %w", cfg.Service, err)
		}
		return nil
	}
	return pollStable(ctx, cfg.Cluster, cfg.Service, rp, cfg.Timeout)
}

func pollStable(ctx context.Context, cluster, service string, rp []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	args := describeServicesArgs(cluster, service, rp)
	for {
		out, err := sparkwing.Exec(ctx, "aws", args...).String()
		if err != nil {
			return fmt.Errorf("ecs: describe service %s: %w", service, err)
		}
		stable, serr := serviceStable([]byte(out))
		if serr != nil {
			return fmt.Errorf("ecs: wait for service %s to stabilize: %w", service, serr)
		}
		if stable {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("ecs: service %s did not stabilize within %s", service, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stablePollInterval):
		}
	}
}

type RollbackConfig struct {
	// Cluster, Service, and TaskDefinition are required; TaskDefinition is
	// typically the ARN Deploy returned.
	Cluster        string
	Service        string
	TaskDefinition string
	Region         string
	AWSProfile     string
}

func (c RollbackConfig) validate() error {
	missing := make([]string, 0, 3)
	if c.Cluster == "" {
		missing = append(missing, "Cluster")
	}
	if c.Service == "" {
		missing = append(missing, "Service")
	}
	if c.TaskDefinition == "" {
		missing = append(missing, "TaskDefinition")
	}
	if len(missing) > 0 {
		return fmt.Errorf("ecs.Rollback: %s required", strings.Join(missing, ", "))
	}
	return nil
}

// Rollback points a service back at a prior task-definition revision and
// waits for it to stabilize.
func Rollback(ctx context.Context, cfg RollbackConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	dry := os.Getenv(dryRunEnv) != ""
	return step.Run(ctx, "ecs rollback", func(ctx context.Context) error {
		rp := regionProfileArgs(cfg.Region, cfg.AWSProfile)
		update := updateServiceArgs(cfg.Cluster, cfg.Service, cfg.TaskDefinition, nil, rp)
		wait := waitStableArgs(cfg.Cluster, cfg.Service, rp)
		if dry {
			echoArgv(ctx, update)
			echoArgv(ctx, wait)
			return nil
		}
		sparkwing.Info(ctx, "rolling %s back to %s", cfg.Service, cfg.TaskDefinition)
		if _, err := sparkwing.Exec(ctx, "aws", update...).Run(); err != nil {
			return fmt.Errorf("ecs: roll back service %s: %w", cfg.Service, err)
		}
		if _, err := sparkwing.Exec(ctx, "aws", wait...).Run(); err != nil {
			return fmt.Errorf("ecs: wait for service %s to stabilize: %w", cfg.Service, err)
		}
		return nil
	})
}
