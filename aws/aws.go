// Package aws holds small AWS-CLI helpers shared across sparks-core
// pipelines. Today that's profile-flag resolution and IRSA detection.
package aws

import (
	"os"
)

// ProfileFlag returns " --profile <name>" for local dev, or "" when
// running under IRSA (IAM Roles for Service Accounts) or when no
// profile is configured at all (AWS_PROFILE unset and defaultProfile
// empty). An empty return leaves credential resolution to the aws
// CLI's own chain: env keys, SSO cache, instance metadata.
//
// Prefer ProfileArgs for argv-shaped exec calls (sparkwing.Exec); use
// ProfileFlag only when splicing into a known-static shell line.
func ProfileFlag(defaultProfile string) string {
	if IsIRSA() {
		return ""
	}
	profile := resolveProfile(defaultProfile)
	if profile == "" {
		return ""
	}
	return " --profile " + profile
}

// ProfileArgs is the argv-shaped variant of ProfileFlag: returns
// {"--profile", "<name>"} for local dev, or an empty slice under IRSA
// or when no profile is configured at all. Append into an aws CLI
// argv directly:
//
//	args := []string{"s3", "sync", src, dst}
//	args = append(args, aws.ProfileArgs(cfg.AWSProfile)...)
//	sparkwing.Exec(ctx, "aws", args...).Run()
func ProfileArgs(defaultProfile string) []string {
	if IsIRSA() {
		return nil
	}
	profile := resolveProfile(defaultProfile)
	if profile == "" {
		return nil
	}
	return []string{"--profile", profile}
}

// resolveProfile prefers AWS_PROFILE over the configured default and
// returns "" when neither is set.
func resolveProfile(defaultProfile string) string {
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		return profile
	}
	return defaultProfile
}

// IsIRSA returns true when running with IAM Roles for Service Accounts
// on EKS. The AWS SDK (and the aws CLI) use the web-identity token
// file without further config, so ProfileFlag must return "" here.
func IsIRSA() bool {
	return os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""
}
