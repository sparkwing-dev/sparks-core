// Package s3 is sparks-core's static-site deploy helper: sync a build
// output directory to an S3 bucket with cache-appropriate headers.
package s3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
)

// StaticSiteConfig configures an S3 static site deployment.
type StaticSiteConfig struct {
	Bucket string
	OutDir string

	// AWSProfile is empty for an assumed role in CI, leaving the aws CLI its
	// own credential chain.
	AWSProfile string

	// ExpectedAccountID, when set, is checked before anything is written. A
	// profile name pins which credentials are selected, not which account
	// they belong to, and federated auth has no profile to name, so this is
	// the only way a caller can say which account it means.
	ExpectedAccountID string

	// Delete removes S3 files no longer in OutDir. Filters apply per pass,
	// so each pass only deletes orphans of its own kind.
	Delete bool

	// Excludes are bucket-relative `aws s3 sync --exclude` globs preserved
	// across both passes, protecting non-OutDir prefixes from Delete.
	Excludes []string
}

// SyncResult reports per-pass upload counts, which callers use to detect an
// internally inconsistent deploy such as new assets with unchanged HTML.
type SyncResult struct {
	AssetUploads int
	HTMLUploads  int
}

// DeployStaticSite syncs a static site build to S3, giving fingerprinted
// assets a 1-year immutable cache and HTML no-cache.
func DeployStaticSite(ctx context.Context, cfg StaticSiteConfig) (SyncResult, error) {
	var res SyncResult
	if cfg.Bucket == "" {
		return res, fmt.Errorf("s3: bucket required")
	}
	if cfg.OutDir == "" {
		cfg.OutDir = "out"
	}
	profileArgs := aws.ProfileArgs(cfg.AWSProfile)
	// safety: excludes must apply to every pass, or --delete on another pass wipes the excluded prefix.
	excludeArgs := make([]string, 0, 2*len(cfg.Excludes))
	for _, ex := range cfg.Excludes {
		excludeArgs = append(excludeArgs, "--exclude", ex)
	}

	sparkwing.Info(ctx, "==> deploy to s3")
	fileCount := countFiles(cfg.OutDir)
	sparkwing.Info(ctx, "syncing %d files from %s/ -> s3://%s", fileCount, cfg.OutDir, cfg.Bucket)

	if dryRun() {
		sparkwing.Info(ctx, "dry run: %s", strings.Join(append([]string{"aws"}, assetSyncArgs(cfg, profileArgs, excludeArgs)...), " "))
		sparkwing.Info(ctx, "dry run: %s", strings.Join(append([]string{"aws"}, htmlCopyArgs(cfg, profileArgs, excludeArgs)...), " "))
		if cfg.Delete {
			sparkwing.Info(ctx, "dry run: %s", strings.Join(append([]string{"aws"}, htmlOrphanSyncArgs(cfg, profileArgs, excludeArgs)...), " "))
		}
		return res, nil
	}

	if err := verifyAccount(ctx, cfg.ExpectedAccountID, cfg.AWSProfile); err != nil {
		return res, err
	}

	assetRes, err := sparkwing.Exec(ctx, "aws", assetSyncArgs(cfg, profileArgs, excludeArgs)...).Run()
	if err != nil {
		return res, err
	}
	res.AssetUploads = countUploads(assetRes.Stdout)

	htmlUploadRes, err := sparkwing.Exec(ctx, "aws", htmlCopyArgs(cfg, profileArgs, excludeArgs)...).Run()
	if err != nil {
		return res, err
	}
	res.HTMLUploads = countUploads(htmlUploadRes.Stdout)

	if cfg.Delete {
		if _, err := sparkwing.Exec(ctx, "aws", htmlOrphanSyncArgs(cfg, profileArgs, excludeArgs)...).Run(); err != nil {
			return res, err
		}
	}

	sparkwing.Info(ctx, "deployed %d files to s3://%s (assets=%d html=%d)",
		res.AssetUploads+res.HTMLUploads, cfg.Bucket, res.AssetUploads, res.HTMLUploads)
	return res, nil
}

func assetSyncArgs(cfg StaticSiteConfig, profileArgs, excludeArgs []string) []string {
	args := []string{"s3", "sync", cfg.OutDir + "/", "s3://" + cfg.Bucket}
	args = append(args, profileArgs...)
	if cfg.Delete {
		args = append(args, "--delete")
	}
	args = append(
		args,
		"--cache-control", "public, max-age=31536000, immutable",
		"--exclude", "*.html",
	)
	return append(args, excludeArgs...)
}

// hack: cp --recursive, not sync -- sync's mtime/size compare can skip changed HTML in cached builds, stranding chunk refs.
func htmlCopyArgs(cfg StaticSiteConfig, profileArgs, excludeArgs []string) []string {
	args := []string{"s3", "cp", cfg.OutDir + "/", "s3://" + cfg.Bucket}
	args = append(args, profileArgs...)
	args = append(
		args,
		"--recursive",
		"--cache-control", "no-cache, no-store, must-revalidate",
		"--exclude", "*",
		"--include", "*.html",
	)
	return append(args, excludeArgs...)
}

func htmlOrphanSyncArgs(cfg StaticSiteConfig, profileArgs, excludeArgs []string) []string {
	args := []string{"s3", "sync", cfg.OutDir + "/", "s3://" + cfg.Bucket}
	args = append(args, profileArgs...)
	args = append(
		args,
		"--delete",
		"--exclude", "*",
		"--include", "*.html",
	)
	return append(args, excludeArgs...)
}

// countUploads reads the `upload: ...` lines both `aws s3 sync` and
// `aws s3 cp` print, one per uploaded file.
func countUploads(stdout string) int {
	n := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "upload:") {
			n++
		}
	}
	return n
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// dryRun is the one setting read from the environment rather than passed
// in, because it can only make a run less destructive.
func dryRun() bool { return os.Getenv("SPARKWING_DRY_RUN") != "" }

// verifyAccount runs before the first write, because the sync deletes and a
// wrong-account deploy is not recoverable by re-running.
func verifyAccount(ctx context.Context, want, profile string) error {
	if want == "" {
		return nil
	}
	res, err := sparkwing.Exec(ctx, "aws", aws.CallerIdentityArgs(profile)...).Run()
	if err != nil {
		return fmt.Errorf("s3: cannot confirm the AWS account before deploying: %w", err)
	}
	got := strings.TrimSpace(res.Stdout)
	if got != want {
		return fmt.Errorf("s3: refusing to deploy, credentials resolve to account %s but the caller expects %s", got, want)
	}
	sparkwing.Info(ctx, "account %s confirmed", got)
	return nil
}
