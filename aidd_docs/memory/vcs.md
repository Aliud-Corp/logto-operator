# VCS

## Setup

- Main branch: `main`
- Platform: GitHub; repository private for now, public later (write everything as if it were public already)

## Branches

- Format: `<type>/<short-description>`
- Never push to `main`, never force-push; every change lands through a pull request. No auto-merge.

## Commits

- Convention: conventional commits, `type(scope): description`, imperative, lowercase, in English.
- **No trace of AI authorship anywhere in the history**: no `Co-Authored-By` line naming an AI, no "Generated with"
  footer, in commits and pull request descriptions alike. The human who commits is the author.

## Commit Strategy

AI should auto commit: `never`
