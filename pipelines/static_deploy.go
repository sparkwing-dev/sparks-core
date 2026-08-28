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

// StaticDeploy is a one-node pipeline that builds a static site, syncs it
// to S3, and optionally invalidates a CloudFront distribution.
type StaticDeploy struct {
	sparkwing.Base

	// BuildCmd is the shell command that produces OutDir.
	BuildCmd string

	// BuildImage, when set, runs BuildCmd in a container instead of on the
	// host. The work dir is copied in and OutDir copied back out via
	// `docker cp`, which works under DinD where bind mounts do not.
	BuildImage string

	// BuildEnvPrefixes forwards every process env var with one of these
	// prefixes into the build container. Ignored when BuildImage is empty.
	BuildEnvPrefixes []string

	// BuildExtraEnv are explicit KEY=VALUE pairs forwarded into the build,
	// in both the docker and host modes.
	BuildExtraEnv map[string]string

	// BuildCacheVolumes maps docker volume names to container mount paths,
	// persisting across builds on the same daemon. Content-addressed caches
	// (npm, go modules) are safe to share; caches embedding site-specific
	// paths (`.next/cache`, Webpack) need a per-site volume name. Ignored
	// when BuildImage is empty.
	BuildCacheVolumes map[string]string

	// Bucket is the target S3 bucket.
	Bucket string

	// OutDir is the build output directory, defaulting to "out".
	OutDir string

	// AWSProfile is empty for an assumed role in CI, where credentials
	// arrive as environment variables and there is no profile to name.
	AWSProfile string

	// ExpectedAccountID, when set, is checked against the account the
	// credentials resolve to before anything is written. A profile name
	// pins which credentials are selected, not which account they belong
	// to, so only this survives a renamed profile and federated auth.
	ExpectedAccountID string

	// CloudFrontID, when set, is invalidated after sync.
	CloudFrontID string

	// URL is logged after a successful deploy.
	URL string

	// SkipBuild bypasses BuildCmd and re-syncs an existing OutDir.
	SkipBuild bool

	// Delete passes --delete to the S3 syncs, removing orphaned objects.
	Delete bool

	// Excludes are glob patterns preserved across both sync passes, to keep
	// non-OutDir prefixes alive under Delete.
	Excludes []string
}

// Plan returns the one-node DAG that runs build and sync as a single step.
func (s *StaticDeploy) Plan(_ context.Context, plan *sparkwing.Plan, _ sparkwing.NoInputs, run sparkwing.RunContext) error {
	sparkwing.Job(plan, run.Pipeline, s.Run)
	return nil
}

// Run executes the build, the S3 sync, and any CloudFront invalidation.
func (s *StaticDeploy) Run(ctx context.Context) error {
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
		Bucket:            s.Bucket,
		OutDir:            s.OutDir,
		AWSProfile:        s.AWSProfile,
		ExpectedAccountID: s.ExpectedAccountID,
		Delete:            s.Delete,
		Excludes:          s.Excludes,
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

// BuildOnly runs just the build phase, for pipelines that validate a build
// without deploying it.
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

// hostBuild ignores BuildEnvPrefixes because inheriting the parent process
// env already covers them.
func (s *StaticDeploy) hostBuild(ctx context.Context) error {
	if len(s.BuildExtraEnv) == 0 {
		return step.Sh(ctx, s.BuildCmd)
	}
	_, err := sparkwing.Exec(ctx, "sh", "-c", s.BuildCmd).EnvMap(s.BuildExtraEnv).Run()
	return err
}

// dockerBuild copies files in and out rather than bind-mounting so it works
// under DinD, where the host filesystem is not shared with the container.
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
