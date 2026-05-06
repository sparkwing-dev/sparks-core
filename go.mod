// Umbrella module declaration. sparks-core is a multi-module monorepo:
// each subdirectory in spark.json is its own Go module with its own
// go.mod, versioned and consumed independently. This root go.mod exists
// only so that proxy.golang.org has a valid module path declaration to
// serve when Go's "matching version" logic auto-fetches the parent
// alongside any sub-module request -- without it, the proxy may fall
// back to stale cached blobs from earlier rename attempts (see
// 2026-05-06 proxy-poisoning incident).
module github.com/sparkwing-dev/sparks-core

go 1.26.0
