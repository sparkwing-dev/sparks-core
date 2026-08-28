// Package aws builds AWS-CLI argv: profile-flag resolution,
// federated-identity detection, and an account check. Every helper is pure;
// callers execute the argv. Nothing here reads AWS_PROFILE to pick a
// profile, because --profile overrides the credentials already in the
// environment, and doing that from an inherited variable would redirect a
// deploy without the pipeline changing.
package aws

import (
	"os"
)

// ProfileFlag returns " --profile <name>", or "" to leave resolution to the
// aws CLI's own chain. Prefer ProfileArgs; use this only when splicing into
// a known-static shell line.
func ProfileFlag(profile string) string {
	if p := resolveProfile(profile); p != "" {
		return " --profile " + p
	}
	return ""
}

// ProfileArgs is the argv-shaped variant of ProfileFlag.
func ProfileArgs(profile string) []string {
	if p := resolveProfile(profile); p != "" {
		return []string{"--profile", p}
	}
	return nil
}

// resolveProfile drops a named profile under IRSA, where a pod has no
// shared credentials file to name and credentials come from the
// web-identity token.
func resolveProfile(profile string) string {
	if IsIRSA() {
		return ""
	}
	return profile
}

// CallerIdentityArgs prints the AWS account id a profile resolves to. Run it
// before a destructive operation: a profile name pins which credentials are
// selected, not which account they belong to, and federated auth has no
// profile to name at all.
func CallerIdentityArgs(profile string) []string {
	args := []string{"sts", "get-caller-identity", "--query", "Account", "--output", "text"}
	return append(args, ProfileArgs(profile)...)
}

// IsIRSA reports whether an EKS web-identity token file is present, which
// the aws CLI uses without further config.
func IsIRSA() bool {
	return os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""
}
