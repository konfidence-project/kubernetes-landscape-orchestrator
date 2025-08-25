# Sample using `kustomize` with Konfidence

This example demonstrates how to use `kustomize` with Konfidence. The controller creates a `OCIRepository` (Flux CR) resource to pull the files packaged as `OCI artifact`, and then it creates a `Kustomization` (Flux CR) to deploy and reconcile the resources.

- Content of `podinfo` directory was taken from [GitHub](https://github.com/stefanprodan/podinfo/tree/master/kustomize).
- See [prerequisites](../README.md) before running this example.

## Upload kustomize files to OCI registry

Local registry (olareg):

```bash
flux push artifact oci://localhost:5000/kustomize/podinfo:v0.1.0 \
  --path="./podinfo" \
  --source="localhost" \
  --revision="v0.1.0@sha1:123456"
```

Remote registry (Harbor demo instance):

```bash
flux push artifact oci://demo.goharbor.io/kden-test/kustomize/podinfo:v0.1.0 \
  --path="./podinfo" \
  --source="localhost" \
  --revision="v0.1.0@sha1:123456"
```

## Create `ArtifactDeployment` CR

Switch to `target-namespace`:

```bash
kubectl config set-context --current --namespace=target-namespace
```

Apply the CR:

```bash
kubectl apply -f ./artifact-deployment-5p1d6x.yaml
```
