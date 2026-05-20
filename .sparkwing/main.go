// Command sparks-core-pipelines is this repo's local pipeline runner.
// It re-exports orchestrator.Main, which dispatches based on argv:
// `sparkwing run <pipeline>` invokes the pipeline; `sparkwing pipeline ...`
// is the agent/operator surface.
package main

import (
	"github.com/sparkwing-dev/sparkwing/pkg/runner"

	_ "sparks-core-pipelines/jobs"
)

func main() { runner.Main() }
