// Package aws holds small AWS-CLI helpers shared across sparks-core
// pipelines. Today that's profile-flag resolution and IRSA detection.
package aws

import (
	"os"
)

// DefaultProfile is used when no AWS_PROFILE is set and we are not
// running under IRSA.
const DefaultProfile = "default"

// ProfileFlag returns " --profile <name>" for local dev, or "" when
// running under IRSA (IAM Roles for Service Accounts). The default
// profile is used when AWS_PROFILE is not set.
//
// Prefer ProfileArgs for argv-shaped exec calls (sparkwing.Exec); use
// ProfileFlag only when splicing into a known-static shell line.
func ProfileFlag(defaultProfile string) string {
	if IsIRSA() {
		return ""
	}
	return " --profile " + resolveProfile(defaultProfile)
}

// ProfileArgs is the argv-shaped variant of ProfileFlag: returns
// {"--profile", "<name>"} for local dev, or an empty slice under IRSA.
// Append into an aws CLI argv directly:
//
//	args := []string{"s3", "sync", src, dst}
//	args = append(args, aws.ProfileArgs(cfg.AWSProfile)...)
//	sparkwing.Exec(ctx, "aws", args...).Run()
func ProfileArgs(defaultProfile string) []string {
	if IsIRSA() {
		return nil
	}
	return []string{"--profile", resolveProfile(defaultProfile)}
}

func resolveProfile(defaultProfile string) string {
	if defaultProfile == "" {
		defaultProfile = DefaultProfile
	}
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = defaultProfile
	}
	return profile
}

// IsIRSA returns true when running with IAM Roles for Service Accounts
// on EKS. The AWS SDK (and the aws CLI) use the web-identity token
// file without further config, so ProfileFlag must return "" here.
func IsIRSA() bool {
	return os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""
}
