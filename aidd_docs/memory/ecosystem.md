# Ecosystem

```mermaid
flowchart LR
  Human([Human])
  Agent([Agent])
  App([App])
  GitHub["GitHub · vcs.md"]
  Actions["GitHub Actions · deployment.md"]
  Logto["Logto · integration.md"]

  Human -- cli --> GitHub
  Agent -- cli --> GitHub
  App -- http --> Logto
  Agent -- "cli, local Logto only" --> Logto
  GitHub -- "push, pull request" --> Actions
```
