package pipelines

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
	"github.com/sparkwing-dev/sparks-core/s3"
	"github.com/sparkwing-dev/sparks-core/step"
)

// StaticDeploy is a one-node pipeline that builds a static site,
// syncs to S3, and optionally invalidates a CloudFront distribution.
// Register via sparkwing.Register with your preferred pipeline name.
//
// Example:
//
//	func init() {
//	    sparkwing.Register("build-deploy", func() any {
//	        return &sparks.StaticDeploy{
//	            BuildCmd:   "npm ci && npm run build",
//	            BuildImage: "node:22-alpine",
//	            Bucket:     "my-website-bucket",
//	            URL:        "https://example.com",
//	            BuildEnvPrefixes: []string{"NEXT_PUBLIC_", "NEXT_EXPORT"},
//	            BuildExtraEnv: map[string]string{
//	                "NEXT_EXPORT": "1",
//	            },
//	        }
//	    })
//	}
type StaticDeploy struct {
	sparkwing.Base

	// BuildCmd is the shell command that produces OutDir.
	BuildCmd string

	// BuildImage, when set, runs BuildCmd inside a Docker container
	// rather than on the host. Needed when the laptop / runner
	// doesn't have the toolchain (e.g. Node) installed locally --
	// typical for Next.js sites on a sparkwing runner pod.
	//
	// When set, the work directory is copied into the container via
	// `docker cp`, the build runs, and OutDir is copied back out.
	// That layout works in DinD environments where host bind mounts
	// don't share the runner filesystem.
	BuildImage string

	// BuildEnvPrefixes is a list of env-var prefixes. Any variable in
	// the current process environment whose name starts with one of
	// these prefixes is forwarded into the build container. Typical
	// use: `["NEXT_PUBLIC_", "NEXT_EXPORT"]` for Next.js sites.
	// Ignored when BuildImage is empty.
	BuildEnvPrefixes []string

	// BuildExtraEnv are explicit KEY=VALUE pairs forwarded into the
	// build subprocess. Useful for injecting defaults that should
	// apply even when the caller hasn't set them in the process env
	// (e.g. `NEXT_EXPORT=1`). Honored in both modes: docker (added as
	// `-e KEY=VALUE` flags on the build container) and host (appended
	// to the build subprocess env on top of the inherited process env).
	BuildExtraEnv map[string]string

	// BuildCacheVolumes maps docker volume names to container mount
	// paths. Each entry becomes a `-v <name>:<path>` on the build
	// container. Volumes persist across builds on the same Docker
	// daemon -- on DinD this means the cache survives between
	// pipeline runs as long as the DinD PVC is intact.
	//
	// Typical uses (pick per language):
	//   "sparks-npm":     "/root/.npm"        (node/npm ci)
	//   "sparks-yarn":    "/usr/local/share/.cache/yarn" (yarn)
	//   "sparks-bundle":  "/usr/local/bundle" (ruby/bundler)
	//   "sparks-gomod":   "/go/pkg/mod"       (go)
	//   "sparks-pip":     "/root/.cache/pip"  (python/pip)
	//
	// Content-addressed caches (npm, go modules) are safe to share
	// across consumers. Build caches that embed site-specific paths
	// (Next.js `.next/cache`, Webpack) should use a per-site volume
	// name to avoid pollution.
	//
	// Ignored when BuildImage is empty.
	BuildCacheVolumes map[string]string

	// Bucket is the target S3 bucket.
	Bucket string

	// OutDir is the build output directory. Defaults to "out".
	OutDir string

	// AWSProfile is the profile used for aws CLI invocations.
	// Required - callers must pass an explicit profile name. Empty
	// AWSProfile fails Run() with an error rather than silently
	// falling back to the literal "default" profile, which has
	// surprised consumers when the local AWS config doesn't have
	// kikd creds (or any creds) under that name.
	AWSProfile string

	// CloudFrontID, when set, triggers a cache invalidation against
	// the named distribution after sync.
	CloudFrontID string

	// URL is the deployed site URL; logged after a successful deploy
	// so the pipeline log tells you where the change landed.
	URL string

	// SkipBuild bypasses BuildCmd. Useful when a previous pipeline
	// already produced OutDir and you only want to re-sync.
	SkipBuild bool

	// Delete passes --delete to the S3 syncs so orphaned objects in
	// the bucket are removed when no matching file exists in OutDir.
	Delete bool

	// Excludes is a list of glob patterns preserved across both sync
	// passes. Use with Delete=true to keep non-OutDir prefixes alive
	// (e.g. release tarballs uploaded by a separate pipeline that
	// shares the bucket).
	Excludes []string
}

// Plan returns the one-node DAG that runs build + sync as a single
// step. Consumers that want per-phase DAG nodes (build cached
// separately, sync as its own node) can implement Plan() on their
// outer struct and call into StaticDeploy.Run or .BuildOnly directly
// instead of embedding.
func (s *StaticDeploy) Plan(_ context.Context, plan *sparkwing.Plan, _ sparkwing.NoInputs, run sparkwing.RunContext) error {
	sparkwing.Job(plan, run.Pipeline, s.Run)
	return nil
}

// Run executes build (optional) + S3 sync (+ CloudFront invalidation
// when configured).
func (s *StaticDeploy) Run(ctx context.Context) error {
	if s.AWSProfile == "" {
		return fmt.Errorf("StaticDeploy: AWSProfile is required")
	}
	if s.OutDir == "" {
		s.OutDir = "out"
	}

	if s.BuildCmd != "" && !s.SkipBuild {
		sparkwing.Info(ctx, "==> build image=%s cmd=%q", s.BuildImage, s.BuildCmd)
		if err := s.BuildOnly(ctx); err != nil {
			return err
		}
	} else if s.SkipBuild {
		sparkwing.Info(ctx, "==> build (skipped via SkipBuild)")
	}

	// safety: verify chunk refs before the S3 sync -- the asset --delete would otherwise strand HTML that points at missing chunks.
	if err := verifyHTMLChunkRefs(s.OutDir); err != nil {
		return err
	}

	sparkwing.Info(ctx, "==> sync s3 bucket=%s dir=%s", s.Bucket, s.OutDir)
	syncRes, err := s3.DeployStaticSite(ctx, s3.StaticSiteConfig{
		Bucket:     s.Bucket,
		OutDir:     s.OutDir,
		AWSProfile: s.AWSProfile,
		Delete:     s.Delete,
		Excludes:   s.Excludes,
	})
	if err != nil {
		return err
	}
	// safety: assets uploaded with zero HTML means the CLI dropped copies -- surface it rather than ship a partial deploy.
	if syncRes.AssetUploads > 0 && syncRes.HTMLUploads == 0 {
		sparkwing.Warn(ctx,
			"%d asset uploads but 0 HTML uploads - unexpected with cp-based HTML phase. "+
				"Check `aws s3 cp` stdout parsing in s3.DeployStaticSite.",
			syncRes.AssetUploads)
	}

	if s.CloudFrontID != "" {
		sparkwing.Info(ctx, "==> cloudfront invalidate id=%s", s.CloudFrontID)
		invalidateArgs := []string{
			"cloudfront", "create-invalidation",
			"--distribution-id", s.CloudFrontID,
		}
		invalidateArgs = append(invalidateArgs, aws.ProfileArgs(s.AWSProfile)...)
		invalidateArgs = append(invalidateArgs, "--paths", "/*")
		if err := step.Run(ctx, "invalidate cloudfront", func(ctx context.Context) error {
			return step.Exec(ctx, "aws", invalidateArgs...)
		}); err != nil {
			return err
		}
	}

	if s.URL != "" {
		sparkwing.Info(ctx, "deployed to %s", s.URL)
	}
	return nil
}

// BuildOnly runs just the build phase (docker-containerized when
// BuildImage is set, otherwise a plain shell exec of BuildCmd). Used
// by "check"-style pipelines that want to validate the build without
// pushing to S3 or kicking a CloudFront invalidation.
//
// Defaults are applied here so callers that only invoke BuildOnly
// (never Run) still get a sane OutDir.
func (s *StaticDeploy) BuildOnly(ctx context.Context) error {
	if s.OutDir == "" {
		s.OutDir = "out"
	}
	var runBuild func(context.Context) error
	if s.BuildImage != "" {
		runBuild = s.dockerBuild
	} else {
		runBuild = s.hostBuild
	}
	return step.Run(ctx, "build", runBuild)
}

// hostBuild runs BuildCmd as a plain shell exec on the host, with
// BuildExtraEnv merged into the subprocess env. BuildEnvPrefixes are
// already covered by inheriting the parent process env.
func (s *StaticDeploy) hostBuild(ctx context.Context) error {
	if len(s.BuildExtraEnv) == 0 {
		return step.Sh(ctx, s.BuildCmd)
	}
	_, err := sparkwing.Exec(ctx, "sh", "-c", s.BuildCmd).EnvMap(s.BuildExtraEnv).Run()
	return err
}

// dockerBuild runs BuildCmd inside BuildImage, copying the work dir
// in via `docker cp` and copying OutDir back out after a successful
// build. The cp-based path (vs bind mount) is chosen so this works
// under DinD where the host filesystem isn't shared with the build
// container.
func (s *StaticDeploy) dockerBuild(ctx context.Context) error {
	workDir := sparkwing.WorkDir()

	createArgs := []string{"create", "-w", "/work"}

	for _, e := range os.Environ() {
		eq := strings.Index(e, "=")
		if eq < 0 {
			continue
		}
		name := e[:eq]
		if s.envVarMatches(name) {
			createArgs = append(createArgs, "-e", e)
		}
	}
	for k, v := range s.BuildExtraEnv {
		createArgs = append(createArgs, "-e", k+"="+v)
	}
	for name, path := range s.BuildCacheVolumes {
		createArgs = append(createArgs, "-v", name+":"+path)
	}
	createArgs = append(createArgs, s.BuildImage, "sh", "-c", s.BuildCmd)

	if _, err := sparkwing.Exec(ctx, "docker", "pull", s.BuildImage).Run(); err != nil {
		return err
	}

	containerID, err := sparkwing.Exec(ctx, "docker", createArgs...).String()
	if err != nil {
		return err
	}
	if containerID == "" {
		return fmt.Errorf("docker create returned empty container id")
	}

	defer func() {
		_, _ = sparkwing.Exec(ctx, "docker", "rm", "-f", containerID).Run()
	}()

	if _, err := sparkwing.Exec(ctx, "docker", "cp", workDir+"/.", containerID+":/work").Run(); err != nil {
		return err
	}
	if _, err := sparkwing.Exec(ctx, "docker", "start", "-a", containerID).Run(); err != nil {
		return fmt.Errorf("build failed in %s: %w", s.BuildImage, err)
	}
	// safety: wipe OutDir first -- docker cp nests src under an existing dir, which would corrupt the S3 key layout.
	hostOut := filepath.Join(workDir, s.OutDir)
	if err := os.RemoveAll(hostOut); err != nil {
		return fmt.Errorf("clean host %s before copy-back: %w", s.OutDir, err)
	}
	if _, err := sparkwing.Exec(ctx, "docker", "cp",
		containerID+":/work/"+s.OutDir, hostOut).Run(); err != nil {
		return fmt.Errorf("copy %s back from build container: %w", s.OutDir, err)
	}
	return nil
}

func (s *StaticDeploy) envVarMatches(name string) bool {
	for _, p := range s.BuildEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
