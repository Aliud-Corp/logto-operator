# logto-operator

Manages the whole configuration of a self-hosted Logto (OSS) as code, **through Logto's Management API only** (never
a direct database write): a CLI (`logtoctl`) and a Kubernetes operator (`LogtoConfig` CRD) on one engine. The design
and its milestones: [docs/plan.md](docs/plan.md); read it before
writing engine code.

## Rules

- Everything written here is English (code, docs, commits). Conventional commits.
- The repository is public: nothing internal to the studio in any file or commit (internal hostnames,
  infrastructure, private repository paths, real ids, e-mails, secrets).
- No trace of AI authorship: no `Co-Authored-By` line naming an AI, no "Generated with" footer, in commits or pull
  requests.
- `task` is the only entry point; CI runs `task` targets and nothing else. Every tool is pinned with mise.
- Generated code (`*.gen.go`) is never edited by hand and never retyped elsewhere: regenerate it, import it.
- Never run anything against a deployed Logto (stag, prod).
- Secrets never in argv, logs, test output or committed fixtures.
- Tests first; a guard must be shown failing once.

## Memory Management

Agent memory and traces live in `aidd_docs/`; documentation for people lives in `docs/`.

Read only task-relevant context and linked memory files; complete required reads.

<!-- aidd_project_memory:start -->

@aidd_docs/memory/architecture.md
@aidd_docs/memory/cli.md
@aidd_docs/memory/codebase-map.md
@aidd_docs/memory/coding-assertions.md
@aidd_docs/memory/deployment.md
@aidd_docs/memory/ecosystem.md
@aidd_docs/memory/infra.md
@aidd_docs/memory/integration.md
@aidd_docs/memory/project-brief.md
@aidd_docs/memory/testing.md
@aidd_docs/memory/vcs.md

<!-- aidd_project_memory:end -->

- Load `aidd_docs/memory/external/*` only when asked.
- Load `aidd_docs/memory/internal/*` when relevant.
