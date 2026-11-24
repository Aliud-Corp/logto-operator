# VCS

## Setup

- Main branch: `main`
- Platform: GitHub; repository public (since 2026-11-23): everything written is read by anyone

## Branches

- Format: `<type>/<short-description>`
- Never push to `main`, never force-push; every change lands through a pull request. No auto-merge.
- Enforced by rulesets: `main` takes pull requests only (squash merge, no force-push, no deletion), with the `check`
  status green; tags `v*` are created, moved or deleted only by the release App (and repository admins).
- Squash merge, the commit titled by the pull request (`PR_TITLE`, empty body): the pull request title is the
  conventional commit release-please reads. A merge commit carried the title again in its body (the only body GitHub
  allows with a "Merge pull request" title), and release-please listed every change twice (0.2.0, 0.2.1).

## Commits

- Convention: conventional commits, `type(scope): description`, imperative, lowercase, in English.
- **No trace of AI authorship anywhere in the history**: no `Co-Authored-By` line naming an AI, no "Generated with"
  footer, in commits and pull request descriptions alike. The human who commits is the author.

## Commit Strategy

AI should auto commit: `never`
