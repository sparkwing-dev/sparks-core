// Package aws holds small AWS-CLI helpers shared across sparks-core
// pipelines: profile-flag resolution, federated-identity detection, and
// the argv for an account check.
//
// Every helper here is pure. They read the environment and build argv,
// and they never shell out, which is why this module has no
// dependencies. Callers execute the argv.
//
// The caller names the profile. Nothing here reads AWS_PROFILE to pick
// one, because passing --profile overrides the credentials already in
// the environment, and doing that from an inherited variable redirects
// a deploy without the pipeline changing. The aws CLI reads AWS_PROFILE
// on its own with the right precedence, so the way to honor it is to
// pass no --profile at all. See the environment rules in the repo
// README.
package aws

import (
	"os"
)

// ProfileFlag returns " --profile <name>" when the caller named a
// profile, and "" otherwise. An empty return leaves credential
// resolution to the aws CLI's own chain: environment keys, AWS_PROFILE,
// the shared credentials file, SSO cache, and instance metadata.
//
// Prefer ProfileArgs for argv-shaped exec calls (sparkwing.Exec); use
// ProfileFlag only when splicing into a known-static shell line.
func ProfileFlag(profile string) string {
	if p := resolveProfile(profile); p != "" {
		return " --profile " + p
	}
	return ""
}

// ProfileArgs is the argv-shaped variant of ProfileFlag: returns
// {"--profile", "<name>"} when the caller named a profile, and nil
// otherwise. Append into an aws CLI argv directly:
//
//	args := []string{"s3", "sync", src, dst}
//	args = append(args, aws.ProfileArgs(cfg.AWSProfile)...)
//	sparkwing.Exec(ctx, "aws", args...).Run()
func ProfileArgs(profile string) []string {
	if p := resolveProfile(profile); p != "" {
		return []string{"--profile", p}
	}
	return nil
}

// resolveProfile returns the profile to pass, or "" for none. A named
// profile is dropped under IRSA because a pod has no shared credentials
// file to name, and its credentials come from the web-identity token
// instead.
func resolveProfile(profile string) string {
	if IsIRSA() {
		return ""
	}
	return profile
}

// CallerIdentityArgs returns the argv that prints the AWS account id the
// given profile resolves to. Callers run it before a destructive
// operation and compare the output to the account they expect, because
// a profile name pins which credentials get selected and not which
// account they belong to. Under federated auth there is no profile to
// name at all, so the account check is the only statement of intent
// available.
//
//	out, _ := sparkwing.Exec(ctx, "aws", aws.CallerIdentityArgs(profile)...).Capture()
func CallerIdentityArgs(profile string) []string {
	args := []string{"sts", "get-caller-identity", "--query", "Account", "--output", "text"}
	return append(args, ProfileArgs(profile)...)
}

// IsIRSA returns true when running with IAM Roles for Service Accounts
// on EKS. It reports a fact about the machine, not a choice: the aws
// CLI uses the web-identity token file without further config, so
// resolveProfile drops any named profile here.
func IsIRSA() bool {
	return os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""
}
