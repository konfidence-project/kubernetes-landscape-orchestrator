# Sample using a Helm chart with Konfidence

This example demonstrates how to use Helm charts with Konfidence. The controller creates a `HelmRepository` (Flux CR) resource to pull the Helm chart, and then it creates a `HelmRelease` (Flux CR) to install it.

- Content of `podinfo` directory was taken from [GitHub](https://github.com/stefanprodan/podinfo/tree/master/charts).
- See [prerequisites](../README.md) before running this example.

## Upload Helm chart to OCI registry

Local registry (olareg):

```bash
helm package ./podinfo
helm push ./podinfo-6.9.1.tgz oci://localhost:5000/helm-charts
```

Remote registry (Harbor demo instance):

```bash
helm package ./podinfo
helm registry login demo.goharbor.io --username <username> --password <password>
helm push ./podinfo-6.9.1.tgz oci://demo.goharbor.io/kden-test/helm-charts
```

## Create `ArtifactDeployment` CR

Switch to `target-namespace`:

```bash
kubectl config set-context --current --namespace=target-namespace
```

Apply the CR:

```bash
kubectl apply -f ./artifact-deployment-6fa82f.yaml
```

## Create `VectorAssignment` CR

Apply the CR:

```bash
kubectl apply -f ./vector-assignment-6fa82f.yaml
```
