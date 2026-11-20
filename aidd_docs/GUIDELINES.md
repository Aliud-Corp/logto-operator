# AI Operating Guidelines

How this team drives AI coding assistants on this project.

## House rules

- The AI never commits, pushes or opens a pull request unless asked, and never adds an AI attribution line.
- Never a call to a deployed Logto (stag, prod): the fake, a local Logto, or the throwaway Logto of a CI job.
- Generated code is regenerated, never edited; a value the generator produces is imported, never retyped.
- Nothing internal to the studio in any file: the repository will be public.

## Validation depth

- A docs-only change: read it back against the code it describes.
- Any code change: the coding assertions in `memory/coding-assertions.md` pass before the change is called done.
- Before a merge: the CI is green on the last push, not on an earlier one.

## When the AI drifts

- Stop, restate the objective in one sentence and the constraint it broke, then resume from the last green state.

For the general AIDD playbook (planning, review loops, prompting and context hygiene, anti-patterns), see the framework docs: <https://github.com/ai-driven-dev/framework>.
