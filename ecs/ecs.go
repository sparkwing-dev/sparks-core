// Package ecs is sparks-core's ECS/Fargate rollout helper: register a
// new task-definition revision from the running one with a swapped
// container image, point the service at it, and wait for the service to
// stabilize -- returning the prior task-definition ARN so a failed
// post-deploy check can roll the service back.
//
// Deploy returns that prior ARN; feed it into Rollback from a
// Job.OnFailure hook. Rollback is a plain update-service back to the
// captured revision, shaped to plug straight into OnFailure.
//
// All AWS work shells out to the `aws` CLI (assumed present) as explicit
// argv through the sparkwing exec helpers; profile/IRSA resolution comes
// from the aws module.
//
// Dry-run: when cfg.DryRun is set or SPARKWING_DRY_RUN is non-empty, the
// mutating rollout (register-task-definition, update-service) does not
// touch AWS. Both Deploy and Rollback echo the exact `aws` argv they
// would run and return success. The describe-task-definition read is
// skipped too rather than executed for real, so a dry run stays green
// without AWS credentials or a live service -- which is what the
// template verify gate relies on. Deploy therefore returns an empty
// prior ARN under dry-run.
package ecs

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// dryRunEnv toggles command-echo mode for every cloud-mutating block in
// sparks-core; a non-empty value skips execution and logs argv.
const dryRunEnv = "SPARKWING_DRY_RUN"

// registerReadOnlyKeys are fields describe-task-definition returns that
// register-task-definition rejects on input; they are stripped before a
// revision is re-registered.
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

// DeployConfig drives Deploy.
type DeployConfig struct {
	// Cluster is the ECS cluster the service runs in. Required.
	Cluster string
	// Service is the ECS service to update. Required.
	Service string
	// TaskFamily is the task-definition family; the current revision is
	// described and re-registered with the fresh image. Required.
	TaskFamily string
	// ContainerName is the container within the task definition whose
	// image is swapped. Required.
	ContainerName string
	// Image is the full image reference (registry/name:tag) to roll to.
	// Required.
	Image string
	// Region is the AWS region of the cluster. Empty omits --region and
	// lets the aws CLI resolve it from the environment or config.
	Region string
	// AWSProfile is the profile for local runs. Empty resolves via
	// AWS_PROFILE, or is dropped entirely under IRSA. See aws.ProfileArgs.
	AWSProfile string
	// DryRun echoes the aws argv without executing, same as setting
	// SPARKWING_DRY_RUN. Either signal activates dry-run.
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

// Deploy rolls a service to a new image: it describes the family's
// current task definition, re-registers it as a fresh revision with the
// container image swapped, updates the service to that revision, and
// waits for the service to reach a stable state. It returns the prior
// task-definition ARN so a failing post-deploy check can hand it to
// Rollback.
//
// Under dry-run (DeployConfig.DryRun or SPARKWING_DRY_RUN) it echoes the
// aws argv and returns an empty prior ARN without contacting AWS.
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
			echoArgv(ctx, registerArgs(dryRunInputRef, rp))
			echoArgv(ctx, updateServiceArgs(cfg.Cluster, cfg.Service, dryRunTaskDefRef, rp))
			echoArgv(ctx, waitStableArgs(cfg.Cluster, cfg.Service, rp))
			return nil
		}
		descJSON, derr := sparkwing.Exec(ctx, "aws", descArgs...).String()
		if derr != nil {
			return derr
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
		defer os.Remove(path)
		newArn, rerr := sparkwing.Exec(ctx, "aws", registerArgs("file://"+path, rp)...).String()
		if rerr != nil {
			return rerr
		}
		sparkwing.Info(ctx, "registered %s (prior %s)", newArn, prior)
		if _, uerr := sparkwing.Exec(ctx, "aws", updateServiceArgs(cfg.Cluster, cfg.Service, newArn, rp)...).Run(); uerr != nil {
			return uerr
		}
		sparkwing.Info(ctx, "waiting for service %s to stabilize", cfg.Service)
		if _, werr := sparkwing.Exec(ctx, "aws", waitStableArgs(cfg.Cluster, cfg.Service, rp)...).Run(); werr != nil {
			return werr
		}
		return nil
	})
	return prevTaskDef, err
}

// RollbackConfig drives Rollback.
type RollbackConfig struct {
	// Cluster is the ECS cluster the service runs in. Required.
	Cluster string
	// Service is the ECS service to roll back. Required.
	Service string
	// TaskDefinition is the revision to restore, typically the ARN
	// Deploy returned. Required.
	TaskDefinition string
	// Region is the AWS region of the cluster. Empty omits --region.
	Region string
	// AWSProfile is the profile for local runs. See aws.ProfileArgs.
	AWSProfile string
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
// waits for it to stabilize. It is Job.OnFailure-shaped: pass the ARN
// Deploy returned as RollbackConfig.TaskDefinition and wire it to a
// failed Verify.
//
// Under SPARKWING_DRY_RUN it echoes the aws argv and returns without
// contacting AWS.
func Rollback(ctx context.Context, cfg RollbackConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	dry := os.Getenv(dryRunEnv) != ""
	return step.Run(ctx, "ecs rollback", func(ctx context.Context) error {
		rp := regionProfileArgs(cfg.Region, cfg.AWSProfile)
		update := updateServiceArgs(cfg.Cluster, cfg.Service, cfg.TaskDefinition, rp)
		wait := waitStableArgs(cfg.Cluster, cfg.Service, rp)
		if dry {
			echoArgv(ctx, update)
			echoArgv(ctx, wait)
			return nil
		}
		sparkwing.Info(ctx, "rolling %s back to %s", cfg.Service, cfg.TaskDefinition)
		if _, err := sparkwing.Exec(ctx, "aws", update...).Run(); err != nil {
			return err
		}
		if _, err := sparkwing.Exec(ctx, "aws", wait...).Run(); err != nil {
			return err
		}
		return nil
	})
}
