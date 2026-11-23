# VCS

## Setup

- Main branch: `main`
- Platform: GitHub; repository public (since 2026-11-23): everything written is read by anyone

## Branches

- Format: `<type>/<short-description>`
- Never push to `main`, never force-push; every change lands through a pull request. No auto-merge.
- Enforced by rulesets: `main` takes pull requests only (merge commits, no force-push, no deletion), with the `check`
  status green; tags `v*` are created, moved or deleted only by the release App (and repository admins).

## Commits

- Convention: conventional commits, `type(scope): description`, imperative, lowercase, in English.
- **No trace of AI authorship anywhere in the history**: no `Co-Authored-By` line naming an AI, no "Generated with"
  footer, in commits and pull request descriptions alike. The human who commits is the author.

## Commit Strategy

AI should auto commit: `never`
