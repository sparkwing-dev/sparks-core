package pipelines

import "fmt"

// NextJSBuild configures the build phase of a StaticDeploy for a
// Next.js static-export site. Strategy picks between two build modes:
//
//   - "container" (default) — strict path: docker build with `npm ci`,
//     the shared sparks-npm download cache, and an optional per-site
//     .next/cache volume. The right choice for reproducible CI/CD.
//   - "host" — fast path: a native `npm install && npm run build` on
//     the runner. No docker, no cache volumes. The right choice for
//     laptop dev targets where speed matters more than reproducibility.
//
// Strategy is declared at construction time, typically wired from the
// pipeline's typed Config (e.g. cfg.NextJSStrategy) so each target's
// values: block in pipelines.yaml picks the mode:
//
//	type DeployConfig struct {
//	    NextJSStrategy string `sw:"nextjs_strategy" default:"container"`
//	}
//
//	cfg := sparkwing.PipelineConfig[DeployConfig](ctx)
//	nb := pipelines.NextJSBuild{
//	    Strategy:  cfg.NextJSStrategy,
//	    SiteCache: "my-site-next",
//	}
//	if err := nb.Apply(&sd); err != nil { return err }
//
// And in pipelines.yaml:
//
//	targets:
//	  prod:      { values: { nextjs_strategy: container } }
//	  local-dev: { values: { nextjs_strategy: host } }
//
// Apply mutates BuildCmd, BuildImage, and BuildCacheVolumes; it leaves
// the rest of StaticDeploy alone so callers can keep BuildEnvPrefixes,
// BuildExtraEnv, Bucket, etc. as direct field assignments.
type NextJSBuild struct {
	// Strategy is "container" or "host". Empty defaults to "container".
	Strategy string

	// SiteCache is the per-site .next/cache volume name (e.g.,
	// "myapp-next"). Mounted at /work/.next/cache when Strategy is
	// "container". Ignored for "host" strategy. Empty disables the
	// per-site Next cache.
	SiteCache string

	// Image overrides the container-strategy build image. Defaults to
	// "node:22-alpine". Ignored for "host" strategy.
	Image string
}

// Apply writes BuildCmd / BuildImage / BuildCacheVolumes onto sd based
// on b.Strategy. Panics on an unknown strategy value (programmer error;
// surfaces at registration time so misconfiguration is impossible to
// ship).
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
		panic(fmt.Sprintf("pipelines.NextJSBuild: unknown strategy %q (want \"host\" or \"container\")", b.Strategy))
	}
}
