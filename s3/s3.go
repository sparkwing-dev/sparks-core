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
	Bucket     string
	OutDir     string
	AWSProfile string

	// Delete removes files in S3 that no longer exist in OutDir.
	// Filters apply, so the asset pass only deletes non-HTML orphans
	// and the HTML pass only deletes HTML orphans. (Implementation:
	// asset pass uses `aws s3 sync --delete`; HTML pass always uses
	// `aws s3 cp --recursive` for upload, then a separate
	// `aws s3 sync --delete` purely for orphan removal -- see the
	// comment in DeployStaticSite for why HTML can't use sync for
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
	if cfg.AWSProfile == "" {
		return res, fmt.Errorf("s3: AWSProfile required")
	}
	profileArgs := aws.ProfileArgs(cfg.AWSProfile)
	// Excludes get re-applied on every sync pass; if they only
	// landed on one pass, --delete on the other pass would wipe the
	// excluded prefix.
	excludeArgs := make([]string, 0, 2*len(cfg.Excludes))
	for _, ex := range cfg.Excludes {
		excludeArgs = append(excludeArgs, "--exclude", ex)
	}

	sparkwing.Info(ctx, "==> deploy to s3")
	fileCount := countFiles(cfg.OutDir)
	sparkwing.Info(ctx, "syncing %d files from %s/ -> s3://%s", fileCount, cfg.OutDir, cfg.Bucket)

	// Assets (JS, CSS, images) - immutable, long-lived cache.
	// `aws s3 sync` is reliable here because bundlers fingerprint
	// asset filenames by content, so any content change yields a new
	// filename that sync uploads unconditionally.
	assetArgs := []string{"s3", "sync", cfg.OutDir + "/", "s3://" + cfg.Bucket}
	assetArgs = append(assetArgs, profileArgs...)
	if cfg.Delete {
		assetArgs = append(assetArgs, "--delete")
	}
	assetArgs = append(
		assetArgs,
		"--cache-control", "public, max-age=31536000, immutable",
		"--exclude", "*.html",
	)
	assetArgs = append(assetArgs, excludeArgs...)
	assetRes, err := sparkwing.Exec(ctx, "aws", assetArgs...).Run()
	if err != nil {
		return res, err
	}
	res.AssetUploads = countUploads(assetRes.Stdout)

	// HTML uploads use `aws s3 cp --recursive` rather than
	// `aws s3 sync` because HTML filenames are stable across builds
	// (e.g. /projects/foo/index.html). `sync` decides whether to
	// upload by comparing local mtime + size against S3, and both
	// signals can lie in a containerized build:
	//   - mtime: build caches (Next.js .next/cache, restored
	//     volumes) can preserve the prior build's mtimes even when
	//     content was regenerated;
	//   - size: HTML often differs by a single chunk-hash filename
	//     swap whose old/new lengths happen to match.
	// When both signals match, `sync` silently skips the HTML pass
	// while the asset pass `--delete`s the chunks the live HTML
	// still references -- the ISS-034 failure mode. `cp --recursive`
	// uploads unconditionally, eliminating the comparison entirely.
	// Cost is negligible: HTML is small text, headers are
	// `no-cache`, and the deploy already invalidates CloudFront.
	htmlArgs := []string{"s3", "cp", cfg.OutDir + "/", "s3://" + cfg.Bucket}
	htmlArgs = append(htmlArgs, profileArgs...)
	htmlArgs = append(
		htmlArgs,
		"--recursive",
		"--cache-control", "no-cache, no-store, must-revalidate",
		"--exclude", "*",
		"--include", "*.html",
	)
	htmlArgs = append(htmlArgs, excludeArgs...)
	htmlUploadRes, err := sparkwing.Exec(ctx, "aws", htmlArgs...).Run()
	if err != nil {
		return res, err
	}
	res.HTMLUploads = countUploads(htmlUploadRes.Stdout)

	// Second HTML pass: sync only for `--delete` semantics, so HTML
	// pages removed from the build also get pruned from S3. Uploads
	// are no-ops here because `cp` above just refreshed every
	// object's S3 mtime to "now", which is newer than any local
	// file. Skipped entirely when Delete is false.
	if cfg.Delete {
		htmlSyncArgs := []string{"s3", "sync", cfg.OutDir + "/", "s3://" + cfg.Bucket}
		htmlSyncArgs = append(htmlSyncArgs, profileArgs...)
		htmlSyncArgs = append(
			htmlSyncArgs,
			"--delete",
			"--exclude", "*",
			"--include", "*.html",
		)
		htmlSyncArgs = append(htmlSyncArgs, excludeArgs...)
		if _, err := sparkwing.Exec(ctx, "aws", htmlSyncArgs...).Run(); err != nil {
			return res, err
		}
	}

	sparkwing.Info(ctx, "deployed %d files to s3://%s (assets=%d html=%d)",
		res.AssetUploads+res.HTMLUploads, cfg.Bucket, res.AssetUploads, res.HTMLUploads)
	return res, nil
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
