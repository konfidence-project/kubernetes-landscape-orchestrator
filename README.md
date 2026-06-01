[![REUSE status](https://api.reuse.software/badge/github.com/konfidence-project/kubernetes-landscape-orchestrator)](https://api.reuse.software/info/github.com/konfidence-project/kubernetes-landscape-orchestrator)

# kubernetes-landscape-orchestrator

## About this project

`kubernetes-landscape-orchestrator` is the Kubernetes deployment implementation for the [Konfidence](https://github.com/konfidence-project/konfidence) platform. It handles the three phases of a vector rollout on a Star cluster: deploying artifacts via Flux, running post-deployment migrations (tasks) and executing activations to cut over traffic to the new version.

## Requirements and Setup

The following prerequisites must be installed in the cluster before running the operator. For local development, [Kind](https://kind.sigs.k8s.io/) can be used (`kind create cluster`).

**1. Install Flux CRDs**

Required by the `flux-deployer` controller, which creates and manages `HelmRelease`, `HelmRepository`, `OCIRepository`, and `Kustomization` objects:

```sh
flux install
```

**2. Install Gateway API CRDs**

Required by the `flux-deployer` and `activation-execution` controllers, which create `HTTPRoute` objects:

```sh
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml
```

**3. Install Konfidence CRDs**

The operator watches resource types from the `star.konfidence.cloud` API group, defined in the [`konfidence-project/konfidence`](https://github.com/konfidence-project/konfidence) repository:

```sh
git clone https://github.com/konfidence-project/konfidence
cd konfidence
make install-star
```

> To install the full Konfidence CRD suite (including `galaxy.konfidence.cloud`), use `make install` instead.

## CLI Usage

The operator is a single command with no subcommands. It starts all selected controllers and runs until terminated.

**Build and run:**

```sh
# Run directly
go run main.go [flags]

# Or build first
make build
./bin/kubernetes-landscape-orchestrator [flags]
```

**Synopsis:**

```
kubernetes-landscape-orchestrator [--controllers <expr>] [--leader-elect] [--health-probe-bind-address <addr>] [--lease-id <id>]
```

### Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--controllers` | `string` | `*` | Comma-separated glob expression selecting which controllers to enable. |
| `--health-probe-bind-address` | `string` | `:8081` | The address the health probe endpoint binds to. |
| `--leader-elect` | `bool` | `false` | Enable leader election to ensure only one active controller manager at a time. |
| `--lease-id` | `string` | `orchestrator.konfidence.cloud` | The ID used for leader election. |

### Available Controllers

| Flag name | Description |
|---|---|
| `FluxDeployer` | Deploys Helm charts and Kustomize manifests via FluxCD primitives. |
| `TaskExecution` | Reconciles `TaskExecution` resources. |
| `ActivationExecution` | Reconciles `ActivationTaskExecution` resources. |

### `--controllers` Examples

The `--controllers` flag accepts a comma-separated list of glob tokens. A leading `!` negates a token. Evaluation is set-based and order-independent.

| Expression                          | Effect |
|-------------------------------------|---|
| `*`                                 | Enable all controllers (default) |
| `FluxDeployer`                      | Enable only the `flux-deployer` controller |
| `TaskExecution,ActivationExecution` | Enable only `task-execution` and `activation-execution` |
| `!FluxDeployer,*`                   | Enable all controllers except `flux-deployer` |

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/konfidence-project/<your-project>/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/konfidence-project/<your-project>/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/konfidence-project/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2026 SAP SE or an SAP affiliate company and kubernetes-landscape-orchestrator contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/konfidence-project/<your-project>).
