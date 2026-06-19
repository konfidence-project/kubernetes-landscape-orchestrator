# kubernetes-landscape-orchestrator

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

Konfidence Kubernetes Landscape Orchestrator operator.

**Homepage:** <https://github.com/konfidence-project/kubernetes-landscape-orchestrator>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Konfidence maintainers |  |  |

## Source Code

* <https://github.com/konfidence-project/kubernetes-landscape-orchestrator>

## Requirements

Kubernetes: `>=1.27.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for the controller Pod. |
| containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Container-level security context. |
| controller | object | `{"controllers":"*","healthProbeBindAddress":":8081","leaderElection":true,"leaseId":"orchestrator.konfidence.cloud","metricsBindAddress":":8080"}` | Forwarded as flags to the operator binary. |
| controller.controllers | string | `"*"` | Comma-separated list of sub-controllers, or `*` for all. Examples: `"FluxDeployer"`, `"!FluxDeployer,*"` (all except), `"Task*"`. |
| controller.healthProbeBindAddress | string | `":8081"` | Bind address for the health probe endpoint. |
| controller.leaderElection | bool | `true` | Enable leader election for the controller. |
| controller.leaseId | string | `"orchestrator.konfidence.cloud"` | Lease ID used for leader election. |
| controller.metricsBindAddress | string | `":8080"` | Bind address for the Prometheus `/metrics` endpoint. Set to `"0"` to disable the metrics server entirely (also suppresses the metrics container port, Service, and ServiceMonitor). |
| env | list | `[]` | Additional environment variables to set on the controller container. |
| extraArgs | list | `[]` | Additional command-line arguments to pass to the controller binary. |
| fullnameOverride | string | `""` | Override the fully-qualified app name used in resource names. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.repository | string | `"ghcr.io/konfidence-project/kubernetes-landscape-orchestrator"` | Image repository. |
| image.tag | string | `""` | Image tag. Defaults to `.Chart.AppVersion` when empty. |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries. |
| nameOverride | string | `""` | Override the chart name used in resource names. |
| nodeSelector | object | `{}` | Node selector for the controller Pod. |
| podAnnotations | object | `{}` | Annotations to add to the controller Pod. |
| podDisruptionBudget | object | `{"enabled":false,"maxUnavailable":1,"minAvailable":null}` | PodDisruptionBudget for the controller. Only meaningful when running `replicas >= 2` (with leader election a single active replica handles reconciliation; the others stand by). Disabled by default to match the default `replicas: 1`. |
| podDisruptionBudget.enabled | bool | `false` | Enable the PodDisruptionBudget. |
| podDisruptionBudget.maxUnavailable | int | `1` | Maximum number of pods that may be unavailable. Accepts an integer or percentage string (e.g. `"50%"`). |
| podDisruptionBudget.minAvailable | string | `nil` | Minimum number of pods that must be available. Set exactly one of `minAvailable` or `maxUnavailable`. Accepts an integer or percentage string (e.g. `"50%"`). |
| podLabels | object | `{}` | Labels to add to the controller Pod. |
| replicas | int | `1` | Number of controller replicas. Use `>= 2` with `controller.leaderElection: true` for HA. |
| resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Resource requests and limits for the controller container. |
| securityContext | object | `{"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}}` | Pod-level security context. |
| serviceAccount.annotations | object | `{}` | Annotations to add to the ServiceAccount. |
| serviceAccount.create | bool | `true` | Create a ServiceAccount for the controller. |
| serviceAccount.name | string | `""` | Name of the ServiceAccount to use. Defaults to the fully-qualified app name when empty. |
| serviceMonitor | object | `{"enabled":false,"interval":"30s","labels":{},"metricRelabelings":[],"namespace":"","relabelings":[],"scrapeTimeout":"10s"}` | Prometheus Operator ServiceMonitor for the controller's `/metrics` endpoint. Requires the `monitoring.coreos.com` CRDs to be installed in the cluster; off by default. |
| serviceMonitor.enabled | bool | `false` | Enable the ServiceMonitor resource. |
| serviceMonitor.interval | string | `"30s"` | Scrape interval. |
| serviceMonitor.labels | object | `{}` | Extra labels merged onto the ServiceMonitor — typically the label selector your Prometheus instance uses (e.g. `release: kube-prometheus`). |
| serviceMonitor.metricRelabelings | list | `[]` | Metric relabeling rules applied after scraping. |
| serviceMonitor.namespace | string | `""` | Namespace to create the ServiceMonitor in. Defaults to the release namespace. Override when your Prometheus instance only watches a specific namespace. |
| serviceMonitor.relabelings | list | `[]` | Relabeling rules applied to scraped samples before ingestion. |
| serviceMonitor.scrapeTimeout | string | `"10s"` | Scrape timeout. |
| tolerations | list | `[]` | Tolerations for the controller Pod. |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
