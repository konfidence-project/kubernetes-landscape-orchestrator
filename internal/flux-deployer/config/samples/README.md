# Sample using Konfidence with Flux

This example shows how to deploy the [podinfo](https://github.com/stefanprodan/podinfo) web application using Konfidence and Flux using

- a Helm chart (see `helm` directory)
- template-free YAML files with `kustomize` (see `kustomize` directory)

## Prerequisites

Create a Kind cluster:

```bash
kind create cluster --name kden-flux-deployer
```

Install Flux in that cluster:

```bash
flux install
```

Create a namespace (this is where the application will be deployed):

```bash
kubectl create namespace target-namespace
```

Install the `ArtifactDeployment` CRD:

```bash
git clone https://github.com/konfidence-project/crds.git
make install
```

Run a minimal OCI registry ([olareg](https://github.com/olareg/olareg)) on your machine:

```bash
docker run -d --rm -p 127.0.0.1:5000:5000 ghcr.io/olareg/olareg:edge serve
```
