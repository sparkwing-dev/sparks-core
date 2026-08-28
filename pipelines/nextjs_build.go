package pipelines

import "fmt"

// NextJSBuild configures the build phase of a StaticDeploy for a Next.js
// static-export site. "container" is the reproducible path (docker, `npm
// ci`, cache volumes); "host" is the fast native path for dev targets.
type NextJSBuild struct {
	// Strategy is "container" (default) or "host".
	Strategy string

	// SiteCache is the per-site .next/cache volume name, mounted at
	// /work/.next/cache. Empty disables that cache; ignored for "host".
	SiteCache string

	// Image defaults to "node:22-alpine"; ignored for "host".
	Image string
}

// Apply writes BuildCmd, BuildImage, and BuildCacheVolumes onto sd, leaving
// its other fields alone. It panics on an unknown strategy, which surfaces
// at registration time.
func (b NextJSBuild) Apply(sd *StaticDeploy) {
	switch b.Strategy {
	case "host":
		sd.BuildCmd = "npm install && npm run build"
		sd.BuildImage = ""
		sd.BuildCacheVolumes = nil
	case "container", "":
		sd.BuildCmd = "npm ci && npm run build"
		sd.BuildImage = b.Image
		if sd.BuildImage == "" {
			sd.BuildImage = "node:22-alpine"
		}
		sd.BuildCacheVolumes = map[string]string{"sparks-npm": "/root/.npm"}
		if b.SiteCache != "" {
			sd.BuildCacheVolumes[b.SiteCache] = "/work/.next/cache"
		}
	default:
		panic(fmt.Sprintf("pipelines.NextJSBuild: unknown strategy %q (want \"host\" or \"container\")", b.Strategy)) //nolint:forbidigo // construction-time programmer error
	}
}
