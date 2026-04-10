// Command sparks-core-pipelines is this repo's local pipeline runner.
// It re-exports orchestrator.Main, which dispatches based on argv:
// `wing <pipeline>` invokes the pipeline; `sparkwing pipeline ...`
// is the agent/operator surface.
package main

import (
	"github.com/sparkwing-dev/sparkwing/orchestrator"

	_ "sparks-core-pipelines/jobs"
)

func main() { orchestrator.Main() }
