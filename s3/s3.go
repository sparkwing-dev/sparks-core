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

	// AWSProfile is the profile to pass to the aws CLI. Empty passes
	// no --profile, leaving the CLI its own credential chain, which is
	// what an assumed role in CI needs. See aws.ProfileArgs.
	AWSProfile string

	// ExpectedAccountID, when set, is checked against the account the
	// credentials actually resolve to before anything is written. A
	// profile name pins which credentials get selected and not which
	// account they belong to, and under federated auth there is no
	// profile to name, so this is the only way for a caller to say
	// which account it means. Empty skips the check.
	ExpectedAccountID string

	// Delete removes files in S3 that no longer exist in OutDir.
	// Filters apply, so the asset pass only deletes non-HTML orphans
	// and the HTML pass only deletes HTML orphans. (Implementation:
	// asset pass uses `aws s3 sync --delete`; HTML pass always uses
	// `aws s3 cp --recursive` for upload, then a separate
	// `aws s3 sync --delete` purely for orphan removal -- see the
	// comment on htmlCopyArgs for why HTML can't use sync for
	// upload.)
	Delete bool

	// Excludes is a list of glob patterns (in `aws s3 sync --exclude`
	// syntax) preserved across both sync passes. Combined with
	// Delete, this is how callers protect non-OutDir prefixes (e.g.
	// release artifacts uploaded by a separate pipeline) from getting
	// wiped on the next site deploy. Patterns are bucket-relative.
	Excludes []string
}

// SyncResult reports per-pass upload counts so callers can detect
// suspicious deploys (e.g. asset uploads with zero HTML uploads --
// see ISS-034).
type SyncResult struct {
	AssetUploads int
	HTMLUploads  int
}

// DeployStaticSite syncs a static site build to S3 with cache headers:
//   - Non-HTML assets: 1-year immutable cache (fingerprinted by bundler)
//   - HTML files: no-cache (always serve fresh content)
//
// Returns per-pass upload counts. Callers can use the counts to detect
// internally inconsistent deploys (e.g. new chunks shipped while HTML
// is unchanged).
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

// assetSyncArgs builds the aws argv for the non-HTML asset pass:
// long-lived immutable cache headers, optional --delete.
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

// htmlCopyArgs builds the aws argv for the HTML upload pass.
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

// htmlOrphanSyncArgs builds the aws argv for the HTML orphan-removal
// pass that runs only under Delete (upload happens in htmlCopyArgs).
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

// countUploads counts `upload: ...` lines in stdout. Both
// `aws s3 sync` and `aws s3 cp` print one such line per file
// uploaded to S3 (despite the latter being conceptually a "copy");
// a no-op pass prints none.
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

// dryRun reports whether SPARKWING_DRY_RUN asks for a no-op run. It is
// the one setting read from the environment rather than passed in,
// because it can only make a run less destructive.
func dryRun() bool { return os.Getenv("SPARKWING_DRY_RUN") != "" }

// verifyAccount fails when the resolved credentials belong to an
// account other than want. It runs before the first write, because the
// sync deletes and a wrong-account deploy is not recoverable by
// re-running with the right one.
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
