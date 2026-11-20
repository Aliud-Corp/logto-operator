# Contributing

Thanks for looking. The project is at the design stage: read
[the plan](docs/plan.md) first, and open an issue before any non-trivial change.

## Ground rules

- English everywhere: code, comments, docs, commit messages.
- Conventional commits (`type(scope): description`, imperative, lowercase).
- One concern per pull request; never push to `main`. The person who commits is the author: no `Co-Authored-By`
  line naming a tool or an AI, no "Generated with" footer.
- Never run anything against a Logto you do not own and cannot throw away. Tests use a fake or a local Logto.
- Secrets never in command-line arguments, logs, test output or committed fixtures.
- Generated code is never edited by hand: change its source, regenerate, commit both.
- A new check or guard comes with proof that it fails when it should.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
