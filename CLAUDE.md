<!-- Sparkwing context for AI agents. Paste into CLAUDE.md or AGENTS.md and commit. Refresh after major sparkwing upgrades via `sparkwing info --for-agent`. -->

## Sparkwing

This repo uses **sparkwing** for CI/CD (https://sparkwing.dev). Pipelines are Go
programs in `.sparkwing/`. Ask the binary, don't scrape the repo:

- `sparkwing info -o json` -- context: binary, project, next steps (start here)
- `sparkwing commands` -- full CLI surface as JSON (every verb + every flag)
- `sparkwing pipeline list -o json` -- this repo's pipelines
- `sparkwing run <name>` -- run a pipeline
- `sparkwing docs read --topic <slug>` -- offline docs; full corpus: https://sparkwing.dev/llms-full.txt
