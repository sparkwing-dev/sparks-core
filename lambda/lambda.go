// Package lambda deploys image- and zip-packaged AWS Lambda functions: each
// deploy updates the code, publishes a version, and shifts a named alias to
// it, returning the version the alias held before so Rollback can restore
// it. The alias must already exist. Mutating calls and the current-alias
// read honor DryRun and SPARKWING_DRY_RUN by echoing the aws argv, so a dry
// deploy needs no AWS credentials.
//
// Across the config types below, an empty Region lets the aws CLI resolve it
// from the environment, an empty AWSProfile resolves via AWS_PROFILE or IRSA,
// an empty Alias means "live", and ExtraArgs append verbatim to
// update-function-code.
package lambda

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
	"github.com/sparkwing-dev/sparks-core/step"
)

const (
	defaultAlias   = "live"
	defaultZipPath = "function.zip"
)

type ImageDeployConfig struct {
	// FunctionName and ImageURI are required.
	FunctionName string
	ImageURI     string
	Alias        string
	Region       string
	AWSProfile   string
	ExtraArgs    []string
	DryRun       bool
}

type ZipDeployConfig struct {
	// FunctionName is required.
	FunctionName string
	// ZipPath is relative to the working directory, defaulting to
	// "function.zip".
	ZipPath string
	// ArtifactBucket stages the archive through S3, which archives above the
	// ~50MB direct-upload limit require. Empty uploads inline.
	ArtifactBucket string
	// ArtifactKey defaults to the base name of ZipPath.
	ArtifactKey string
	Alias       string
	Region      string
	AWSProfile  string
	ExtraArgs   []string
	DryRun      bool
}

type RollbackConfig struct {
	// FunctionName and Version are required; Version is typically the
	// prevVersion a preceding deploy returned.
	FunctionName string
	Alias        string
	Version      string
	Region       string
	AWSProfile   string
	DryRun       bool
}

func (c *ImageDeployConfig) applyDefaults() {
	c.Alias = orDefault(c.Alias, defaultAlias)
}

func (c *ZipDeployConfig) applyDefaults() {
	c.ZipPath = orDefault(c.ZipPath, defaultZipPath)
	c.Alias = orDefault(c.Alias, defaultAlias)
	if c.ArtifactBucket != "" && c.ArtifactKey == "" {
		c.ArtifactKey = filepath.Base(c.ZipPath)
	}
}

func (c *RollbackConfig) applyDefaults() {
	c.Alias = orDefault(c.Alias, defaultAlias)
}

// DeployImage points an Image-packaged Lambda at a new image, publishes a
// version, and shifts the alias to it, returning the alias's prior version.
func DeployImage(ctx context.Context, cfg ImageDeployConfig) (prevVersion string, err error) {
	cfg.applyDefaults()
	if cfg.FunctionName == "" {
		return "", fmt.Errorf("lambda: FunctionName required")
	}
	if cfg.ImageURI == "" {
		return "", fmt.Errorf("lambda: ImageURI required")
	}
	err = step.Run(ctx, "deploy lambda (image)", func(ctx context.Context) error {
		profile := aws.ProfileArgs(cfg.AWSProfile)
		dry := dryRunEnabled(cfg.DryRun)
		prev, err := currentAliasVersion(ctx, cfg.FunctionName, cfg.Alias, cfg.Region, profile, dry)
		if err != nil {
			return err
		}
		prevVersion = prev
		sparkwing.Info(ctx, "publishing %s from image %s", cfg.FunctionName, cfg.ImageURI)
		version, err := publishCode(ctx, updateImageCodeArgs(cfg.FunctionName, cfg.ImageURI, cfg.Region, cfg.ExtraArgs, profile), dry)
		if err != nil {
			return err
		}
		return shiftAlias(ctx, cfg.FunctionName, cfg.Alias, version, cfg.Region, profile, dry)
	})
	return prevVersion, err
}

// DeployZip updates a Zip-packaged Lambda's code, publishes a version, and
// shifts the alias to it, returning the alias's prior version.
func DeployZip(ctx context.Context, cfg ZipDeployConfig) (prevVersion string, err error) {
	cfg.applyDefaults()
	if cfg.FunctionName == "" {
		return "", fmt.Errorf("lambda: FunctionName required")
	}
	if cfg.ZipPath == "" {
		return "", fmt.Errorf("lambda: ZipPath required")
	}
	err = step.Run(ctx, "deploy lambda (zip)", func(ctx context.Context) error {
		profile := aws.ProfileArgs(cfg.AWSProfile)
		dry := dryRunEnabled(cfg.DryRun)
		prev, err := currentAliasVersion(ctx, cfg.FunctionName, cfg.Alias, cfg.Region, profile, dry)
		if err != nil {
			return err
		}
		prevVersion = prev

		var codeArgs []string
		if cfg.ArtifactBucket != "" {
			sparkwing.Info(ctx, "staging %s -> s3://%s/%s", cfg.ZipPath, cfg.ArtifactBucket, cfg.ArtifactKey)
			if err := runAWS(ctx, s3StageArgs(cfg.ZipPath, cfg.ArtifactBucket, cfg.ArtifactKey, cfg.Region, profile), dry); err != nil {
				return err
			}
			codeArgs = updateZipS3Args(cfg.FunctionName, cfg.ArtifactBucket, cfg.ArtifactKey, cfg.Region, cfg.ExtraArgs, profile)
		} else {
			codeArgs = updateZipDirectArgs(cfg.FunctionName, cfg.ZipPath, cfg.Region, cfg.ExtraArgs, profile)
		}

		sparkwing.Info(ctx, "publishing %s from %s", cfg.FunctionName, cfg.ZipPath)
		version, err := publishCode(ctx, codeArgs, dry)
		if err != nil {
			return err
		}
		return shiftAlias(ctx, cfg.FunctionName, cfg.Alias, version, cfg.Region, profile, dry)
	})
	return prevVersion, err
}

// Rollback shifts a function's alias back to Version.
func Rollback(ctx context.Context, cfg RollbackConfig) error {
	cfg.applyDefaults()
	if cfg.FunctionName == "" {
		return fmt.Errorf("lambda: FunctionName required")
	}
	if cfg.Version == "" {
		return fmt.Errorf("lambda: Version required")
	}
	return step.Run(ctx, "rollback lambda alias", func(ctx context.Context) error {
		profile := aws.ProfileArgs(cfg.AWSProfile)
		return shiftAlias(ctx, cfg.FunctionName, cfg.Alias, cfg.Version, cfg.Region, profile, dryRunEnabled(cfg.DryRun))
	})
}

func dryRunEnabled(explicit bool) bool {
	return explicit || os.Getenv("SPARKWING_DRY_RUN") != ""
}

// currentAliasVersion skips the read under dry-run so no credentials are
// needed.
func currentAliasVersion(ctx context.Context, fn, alias, region string, profile []string, dry bool) (string, error) {
	if dry {
		sparkwing.Info(ctx, "[dry-run] skipping current-alias read for %s:%s", fn, alias)
		return "", nil
	}
	version, err := sparkwing.Exec(ctx, "aws", getAliasArgs(fn, alias, region, profile)...).String()
	if err != nil {
		return "", fmt.Errorf("read current alias version: %w", err)
	}
	return version, nil
}

func publishCode(ctx context.Context, args []string, dry bool) (string, error) {
	if dry {
		logDryRun(ctx, args)
		return "", nil
	}
	version, err := sparkwing.Exec(ctx, "aws", args...).String()
	if err != nil {
		return "", fmt.Errorf("update function code: %w", err)
	}
	return version, nil
}

// shiftAlias echoes a placeholder under dry-run, where nothing was published.
func shiftAlias(ctx context.Context, fn, alias, version, region string, profile []string, dry bool) error {
	if dry && version == "" {
		version = "<published-version>"
	}
	sparkwing.Info(ctx, "shifting alias %s -> %s", alias, version)
	return runAWS(ctx, updateAliasArgs(fn, alias, version, region, profile), dry)
}

func runAWS(ctx context.Context, args []string, dry bool) error {
	if dry {
		logDryRun(ctx, args)
		return nil
	}
	return step.Exec(ctx, "aws", args...)
}

func logDryRun(ctx context.Context, args []string) {
	sparkwing.Info(ctx, "[dry-run] would run: aws %s", strings.Join(args, " "))
}

func appendRegion(args []string, region string) []string {
	if region == "" {
		return args
	}
	return append(args, "--region", region)
}

func getAliasArgs(fn, alias, region string, profile []string) []string {
	args := []string{
		"lambda", "get-alias",
		"--function-name", fn,
		"--name", alias,
		"--query", "FunctionVersion",
		"--output", "text",
	}
	args = appendRegion(args, region)
	return append(args, profile...)
}

func updateImageCodeArgs(fn, imageURI, region string, extra, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--image-uri", imageURI,
		"--publish",
		"--query", "Version",
		"--output", "text",
	}
	args = appendRegion(args, region)
	args = append(args, extra...)
	return append(args, profile...)
}

func updateZipS3Args(fn, bucket, key, region string, extra, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--s3-bucket", bucket,
		"--s3-key", key,
		"--publish",
		"--query", "Version",
		"--output", "text",
	}
	args = appendRegion(args, region)
	args = append(args, extra...)
	return append(args, profile...)
}

func updateZipDirectArgs(fn, zipPath, region string, extra, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--zip-file", "fileb://" + zipPath,
		"--publish",
		"--query", "Version",
		"--output", "text",
	}
	args = appendRegion(args, region)
	args = append(args, extra...)
	return append(args, profile...)
}

func s3StageArgs(zipPath, bucket, key, region string, profile []string) []string {
	args := []string{
		"s3", "cp",
		zipPath,
		"s3://" + bucket + "/" + key,
	}
	args = appendRegion(args, region)
	return append(args, profile...)
}

func updateAliasArgs(fn, alias, version, region string, profile []string) []string {
	args := []string{
		"lambda", "update-alias",
		"--function-name", fn,
		"--name", alias,
		"--function-version", version,
	}
	args = appendRegion(args, region)
	return append(args, profile...)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
