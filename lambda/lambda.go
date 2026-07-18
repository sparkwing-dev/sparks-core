// Package lambda deploys AWS Lambda functions for both packaging types
// -- container image and zip -- behind one module. Each deploy updates
// the function code, publishes an immutable version, and shifts a named
// alias (e.g. "live") to that version, returning the version the alias
// pointed at beforehand so a failed post-deploy check can roll back.
//
// DeployImage updates an Image-packaged function, pointing it at a new
// --image-uri. DeployZip updates a Zip-packaged function, either staging
// the archive through S3 (set ArtifactBucket, required for archives
// above the direct-upload limit) or uploading it inline with --zip-file.
// Rollback shifts an alias back to a prior version and is shaped for a
// sparkwing Job.OnFailure hook:
//
//	prev, err := lambda.DeployImage(ctx, lambda.ImageDeployConfig{
//	    FunctionName: "checkout", ImageURI: uri, Alias: "live",
//	})
//	// ... on a failed Verify:
//	lambda.Rollback(ctx, lambda.RollbackConfig{
//	    FunctionName: "checkout", Alias: "live", Version: prev,
//	})
//
// Every state-mutating call honors the dry-run contract: when a config's
// DryRun field is set or the SPARKWING_DRY_RUN environment variable is
// non-empty, the exact aws argv is logged and the call returns success
// without touching the cloud. The current-alias read is skipped under
// dry-run so a dry deploy needs no AWS credentials.
//
// Requires the `aws` CLI on PATH. Profile/IRSA resolution comes from the
// aws module; the named alias is assumed to already exist (created out of
// band by the function's infrastructure).
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
	defaultRegion  = "us-west-2"
	defaultZipPath = "function.zip"
)

// ImageDeployConfig drives DeployImage.
type ImageDeployConfig struct {
	// FunctionName is the Lambda function to update. Required.
	FunctionName string
	// ImageURI is the container image the function pulls, including the
	// registry, repository, and tag or digest. Required.
	ImageURI string
	// Alias is shifted to the freshly published version. Defaults to
	// "live".
	Alias string
	// Region is the function's AWS region. Defaults to "us-west-2".
	Region string
	// AWSProfile selects the aws CLI profile for local runs; empty
	// resolves via AWS_PROFILE or IRSA (see the aws module).
	AWSProfile string
	// DryRun logs the argv of every mutating aws call and skips
	// execution, the same effect as a non-empty SPARKWING_DRY_RUN.
	DryRun bool
}

// ZipDeployConfig drives DeployZip.
type ZipDeployConfig struct {
	// FunctionName is the Lambda function to update. Required.
	FunctionName string
	// ZipPath is the deployment archive to upload, relative to the
	// pipeline working directory. Defaults to "function.zip".
	ZipPath string
	// ArtifactBucket, when set, stages the archive through S3 before the
	// code update (required for archives above the ~50MB direct-upload
	// limit). Empty updates code inline with --zip-file.
	ArtifactBucket string
	// ArtifactKey is the S3 object key the archive stages to. Defaults
	// to the base name of ZipPath. Ignored when ArtifactBucket is empty.
	ArtifactKey string
	// Alias is shifted to the freshly published version. Defaults to
	// "live".
	Alias string
	// Region is the function's AWS region. Defaults to "us-west-2".
	Region string
	// AWSProfile selects the aws CLI profile for local runs; empty
	// resolves via AWS_PROFILE or IRSA (see the aws module).
	AWSProfile string
	// DryRun logs the argv of every mutating aws call and skips
	// execution, the same effect as a non-empty SPARKWING_DRY_RUN.
	DryRun bool
}

// RollbackConfig drives Rollback.
type RollbackConfig struct {
	// FunctionName is the Lambda function whose alias moves. Required.
	FunctionName string
	// Alias is the alias to shift back. Defaults to "live".
	Alias string
	// Version is the version to point the alias at, typically the
	// prevVersion a preceding DeployImage/DeployZip returned. Required.
	Version string
	// Region is the function's AWS region. Defaults to "us-west-2".
	Region string
	// AWSProfile selects the aws CLI profile for local runs; empty
	// resolves via AWS_PROFILE or IRSA (see the aws module).
	AWSProfile string
}

func (c *ImageDeployConfig) applyDefaults() {
	c.Alias = orDefault(c.Alias, defaultAlias)
	c.Region = orDefault(c.Region, defaultRegion)
}

func (c *ZipDeployConfig) applyDefaults() {
	c.ZipPath = orDefault(c.ZipPath, defaultZipPath)
	c.Alias = orDefault(c.Alias, defaultAlias)
	c.Region = orDefault(c.Region, defaultRegion)
	if c.ArtifactBucket != "" && c.ArtifactKey == "" {
		c.ArtifactKey = filepath.Base(c.ZipPath)
	}
}

func (c *RollbackConfig) applyDefaults() {
	c.Alias = orDefault(c.Alias, defaultAlias)
	c.Region = orDefault(c.Region, defaultRegion)
}

// DeployImage points an Image-packaged Lambda at a new image, publishes
// a version, and shifts the alias to it. It returns the version the
// alias pointed at before the shift, for a subsequent Rollback.
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
		version, err := publishCode(ctx, updateImageCodeArgs(cfg.FunctionName, cfg.ImageURI, cfg.Region, profile), dry)
		if err != nil {
			return err
		}
		return shiftAlias(ctx, cfg.FunctionName, cfg.Alias, version, cfg.Region, profile, dry)
	})
	return prevVersion, err
}

// DeployZip updates a Zip-packaged Lambda's code, publishes a version,
// and shifts the alias to it. When ArtifactBucket is set the archive is
// staged through S3 first; otherwise it is uploaded inline. It returns
// the version the alias pointed at before the shift, for a Rollback.
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
			codeArgs = updateZipS3Args(cfg.FunctionName, cfg.ArtifactBucket, cfg.ArtifactKey, cfg.Region, profile)
		} else {
			codeArgs = updateZipDirectArgs(cfg.FunctionName, cfg.ZipPath, cfg.Region, profile)
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

// Rollback shifts a function's alias back to Version. It is the
// OnFailure-shaped counterpart to DeployImage/DeployZip: feed it the
// prevVersion the deploy returned. Honors SPARKWING_DRY_RUN.
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
		return shiftAlias(ctx, cfg.FunctionName, cfg.Alias, cfg.Version, cfg.Region, profile, dryRunEnabled(false))
	})
}

// dryRunEnabled reports whether mutating calls should be echoed instead
// of executed: true when the caller set DryRun or SPARKWING_DRY_RUN is
// non-empty.
func dryRunEnabled(explicit bool) bool {
	return explicit || os.Getenv("SPARKWING_DRY_RUN") != ""
}

// currentAliasVersion reads the version the alias currently points at.
// Under dry-run the read is skipped (no credentials needed) and an
// empty string is returned.
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

// publishCode runs an update-function-code invocation and returns the
// published version. Under dry-run it logs the argv and returns "".
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

// shiftAlias moves the alias to version. Under dry-run the version is
// unknown (nothing was published), so a placeholder is echoed.
func shiftAlias(ctx context.Context, fn, alias, version, region string, profile []string, dry bool) error {
	if dry && version == "" {
		version = "<published-version>"
	}
	sparkwing.Info(ctx, "shifting alias %s -> %s", alias, version)
	return runAWS(ctx, updateAliasArgs(fn, alias, version, region, profile), dry)
}

// runAWS runs an aws CLI mutation, or logs its argv and returns nil when
// dry is set.
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

func getAliasArgs(fn, alias, region string, profile []string) []string {
	args := []string{
		"lambda", "get-alias",
		"--function-name", fn,
		"--name", alias,
		"--region", region,
		"--query", "FunctionVersion",
		"--output", "text",
	}
	return append(args, profile...)
}

func updateImageCodeArgs(fn, imageURI, region string, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--image-uri", imageURI,
		"--publish",
		"--region", region,
		"--query", "Version",
		"--output", "text",
	}
	return append(args, profile...)
}

func updateZipS3Args(fn, bucket, key, region string, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--s3-bucket", bucket,
		"--s3-key", key,
		"--publish",
		"--region", region,
		"--query", "Version",
		"--output", "text",
	}
	return append(args, profile...)
}

func updateZipDirectArgs(fn, zipPath, region string, profile []string) []string {
	args := []string{
		"lambda", "update-function-code",
		"--function-name", fn,
		"--zip-file", "fileb://" + zipPath,
		"--publish",
		"--region", region,
		"--query", "Version",
		"--output", "text",
	}
	return append(args, profile...)
}

func s3StageArgs(zipPath, bucket, key, region string, profile []string) []string {
	args := []string{
		"s3", "cp",
		zipPath,
		"s3://" + bucket + "/" + key,
		"--region", region,
	}
	return append(args, profile...)
}

func updateAliasArgs(fn, alias, version, region string, profile []string) []string {
	args := []string{
		"lambda", "update-alias",
		"--function-name", fn,
		"--name", alias,
		"--function-version", version,
		"--region", region,
	}
	return append(args, profile...)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
