// Command sparks-core-pipelines is this repo's local pipeline runner.
package main

import (
	"github.com/sparkwing-dev/sparkwing/pkg/runner"

	_ "sparks-core-pipelines/jobs"
)

func main() { runner.Main() }
