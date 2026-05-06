package pipelines

import "github.com/sparkwing-dev/sparkwing/sparkwing"

// NextJSBuild configures the build phase of a StaticDeploy for a
// Next.js static-export site, branching on the current host class.
//
// On laptop hosts (per sparkwing.CurrentRunConfig().IsLocal) it
// configures a native build with `npm install` against the persistent
// repo-local node_modules — Docker is skipped, no cache volumes are
// needed. On cluster hosts it configures the strict path: a docker
// build with `npm ci`, the shared sparks-npm download cache, and an
// optional per-site .next/cache volume.
//
// Apply mutates BuildCmd, BuildImage, and BuildCacheVolumes; it
// leaves the rest of StaticDeploy alone so callers can keep
// BuildEnvPrefixes, BuildExtraEnv, Bucket, etc. as direct field
// assignments.
//
// Example:
//
//	sd := sparks.StaticDeploy{
//	    BuildEnvPrefixes: []string{"NEXT_PUBLIC_", "NEXT_EXPORT"},
//	    BuildExtraEnv:    map[string]string{"NEXT_EXPORT": "1"},
//	    Bucket:           "my-website-bucket",
//	    URL:              "https://example.com",
//	    Delete:           true,
//	}
//	sparks.NextJSBuild{SiteCache: "my-site-next"}.Apply(&sd)
type NextJSBuild struct {
	// SiteCache is the per-site .next/cache volume name (e.g.,
	// "myapp-next"). Mounted at /work/.next/cache on cluster runs.
	// Empty disables the per-site Next cache (builds still work,
	// just no incremental rebuild speedup between pipeline runs).
	SiteCache string

	// Image overrides the cluster-side build image. Defaults to
	// "node:22-alpine".
	Image string
}

// Apply writes BuildCmd / BuildImage / BuildCacheVolumes onto sd.
func (b NextJSBuild) Apply(sd *StaticDeploy) {
	if sparkwing.CurrentRunConfig().IsLocal {
		sd.BuildCmd = "npm install && npm run build"
		sd.BuildImage = ""
		sd.BuildCacheVolumes = nil
		return
	}
	sd.BuildCmd = "npm ci && npm run build"
	sd.BuildImage = b.Image
	if sd.BuildImage == "" {
		sd.BuildImage = "node:22-alpine"
	}
	sd.BuildCacheVolumes = map[string]string{"sparks-npm": "/root/.npm"}
	if b.SiteCache != "" {
		sd.BuildCacheVolumes[b.SiteCache] = "/work/.next/cache"
	}
}
