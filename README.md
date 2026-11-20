# logto-operator

Manage the whole configuration of a self-hosted Logto (OSS 1.44.0) as code, through Logto's Management API only:
a CLI (`logtoctl plan | apply | check | render`) and a Kubernetes operator (`LogtoConfig`) sharing one engine.

Status: skeleton. Design: [docs/plan.md](docs/plan.md). Contributing: [CONTRIBUTING.md](CONTRIBUTING.md). License: [Apache-2.0](LICENSE).

```bash
mise install      # pinned toolchain
task check        # the CI gate, locally
```
