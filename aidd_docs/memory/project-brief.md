# Project Brief

## What it is

- A tool that manages the whole configuration of a self-hosted Logto (OSS) as code, through Logto's Management API
  only: a CLI (`logtoctl`) and, later, a Kubernetes operator, both on one engine.
- Built first for the studio's own products, and published as open source.

## Why it exists

- Hand-written seeding scripts drift apart, detect no drift, and often write to Logto's database, which cannot be
  done against a deployed Logto. One reviewed desired-state file per Logto instance, applied through the API,
  replaces them.

## Domain language

| Term | Meaning |
| ---- | ------- |
| Management API | Logto's REST API for configuration, reached with an M2M token for resource `https://default.logto.app/api` |
| tenant | `default` holds the product's configuration; `admin` holds the console's own users and apps |
| M2M application | a machine-to-machine client (`client_credentials`); the tool authenticates as one |
| `m-default` | Logto's built-in admin-tenant M2M app that can call the default tenant's Management API |
| desired state / `LogtoConfig` | the declared configuration; also the CRD kind and the CLI file format |
| kind | one family of Logto objects the engine reconciles (applications, resources, connectors, ...) |
| adopt | match an existing Logto object by its natural key instead of creating it (applications by name) |
| outputs | values Logto generates (application ids, M2M secrets, webhook signing keys) that the tool publishes |
| drift | a difference between the desired state and what Logto holds |
| prune | deleting Logto objects of a kind that the desired state no longer declares (opt-in per kind) |

## Key features

- Idempotent `plan` / `apply` / `check` / `render`; an unchanged re-apply writes nothing.
- Ids and secrets are outputs, never inputs; secrets never in argv or logs.
- The design and its milestones: [`../../docs/plan.md`](../../docs/plan.md).
