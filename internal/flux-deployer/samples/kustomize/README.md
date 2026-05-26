# Sample using `kustomize` with Konfidence

This example demonstrates how to use `kustomize` with Konfidence. The controller creates a `OCIRepository` (Flux CR) resource to pull the files packaged as `OCI artifact`, and then it creates a `Kustomization` (Flux CR) to deploy and reconcile the resources.

- Content of `podinfo` directory was taken from [GitHub](https://github.com/stefanprodan/podinfo/tree/master/kustomize).
- See [prerequisites](../README.md) before running this example.

## Upload kustomize files to OCI registry

### Local registry (olareg):

```bash
flux push artifact oci://localhost:5000/kustomize/podinfo:v0.1.0 \
  --path="./podinfo" \
  --source="localhost" \
  --revision="v0.1.0@sha1:123456"
```

### Remote registry (Harbor demo instance):

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

### Pull from local registry (olareg)

Apply the CR `artifact-deployment-5p1d6x.yaml` which pulls from local registry (insecure, without authentication).

```bash
kubectl apply -f ./artifact-deployment-5p1d6x.yaml
```

### Pull from remote registry (Harbor)

The CR `artifact-deployment-je263b.yaml` pulls the kustomize files from Harbor. Before applying, you need to create a pull secret with your Harbor credentials. Add the credentials to the following JSON snippet, base64 encode it, and add it to the data field of the pull secret in the YAML file.

```json
{
  "auths": {
    "https://demo.goharbor.io": {
      "username": "",
      "password": ""
    }
  }
}
```

Afterward, apply the CR `artifact-deployment-je263b.yaml`.

```bash
kubectl apply -f ./artifact-deployment-je263b.yaml
```
