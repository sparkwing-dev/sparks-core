package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
	sparkwingGit "github.com/sparkwing-dev/sparkwing/sparkwing/git"

	"github.com/sparkwing-dev/sparks-core/step"
)

type RevertConfig struct {
	// GitopsRepo is the SSH URL of the gitops repo. Required.
	GitopsRepo string
	// Commit defaults to "HEAD", the most recent deploy.
	Commit    string
	CommitMsg string
	// MaxRetries bounds the pull-and-retry loop on push conflicts,
	// defaulting to 5.
	MaxRetries int
}

// Revert reverts a gitops commit and pushes, leaving ArgoCD to sync the
// cluster back. changed is true only when a revert commit was pushed. Unlike
// Deploy it skips controller authorization, because a recovery action should
// not be gated on the approval path that may have just failed.
func Revert(ctx context.Context, cfg RevertConfig) (changed bool, err error) {
	if cfg.GitopsRepo == "" {
		return false, fmt.Errorf("gitops revert: GitopsRepo required")
	}
	if cfg.Commit == "" {
		cfg.Commit = "HEAD"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.CommitMsg == "" {
		cfg.CommitMsg = "rollback: revert " + cfg.Commit
	}

	err = step.Run(ctx, "rollback (gitops revert)", func(ctx context.Context) error {
		tmpDir := filepath.Join(os.TempDir(), "sparkwing-gitops-revert")
		_ = os.RemoveAll(tmpDir)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		restoreSSH := setSSHEnv(ctx)
		defer restoreSSH()

		// safety: full clone required -- git revert needs the reverted commit's parent.
		if err := sparkwingGit.Clone(ctx, cfg.GitopsRepo, tmpDir); err != nil {
			return err
		}
		if pushRemote := pushTransport(ctx, cfg.GitopsRepo); pushRemote != "" {
			if err := step.Exec(ctx, "git", "-C", tmpDir, "remote", "set-url", "origin", pushRemote); err != nil {
				return err
			}
		}

		for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
			if attempt > 1 {
				sparkwing.Info(ctx, "push failed (attempt %d/%d), pulling and retrying...", attempt-1, cfg.MaxRetries)
				_, _ = sparkwing.Exec(ctx, "git", "-C", tmpDir, "revert", "--abort").Run()
				if err := step.Exec(ctx, "git", "-C", tmpDir, "fetch", "origin", "main"); err != nil {
					return err
				}
				if err := step.Exec(ctx, "git", "-C", tmpDir, "reset", "--hard", "origin/main"); err != nil {
					return err
				}
			}

			if err := step.Exec(ctx, "git", "-C", tmpDir, "revert", "--no-commit", cfg.Commit); err != nil {
				return err
			}
			if err := step.Exec(
				ctx, "git", "-C", tmpDir,
				"-c", "user.name=sparkwing",
				"-c", "user.email=sparkwing@noreply",
				"commit", "-m", cfg.CommitMsg,
			); err != nil {
				return err
			}
			changed = true

			if _, pushErr := sparkwing.Exec(ctx, "git", "-C", tmpDir, "push").Run(); pushErr == nil {
				sparkwing.Info(ctx, "reverted %s", cfg.Commit)
				return nil
			}
		}
		return fmt.Errorf("gitops revert push failed after %d attempts", cfg.MaxRetries)
	})
	return changed, err
}
