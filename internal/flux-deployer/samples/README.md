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

Install istio in that cluster:

```bash
istioctl install
```

Install the Gateway-API CRDs in that cluster:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml
```

Create a namespace (this is where the application will be deployed) and label it for istio sidecar injection:

```bash
kubectl create namespace target-namespace
kubectl label namespace target-namespace istio-injection=enabled
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

## Kubeconfig for remote clusters

If the deployment target is a remote Kubernetes cluster, you need to provide a kubeconfig to the deployer so it can connect to that cluster. The `./create-kubeconfig-secret/create-kubeconfig.sh` script outputs a base64 encoded kubeconfig string by executing the following steps: 

- Step 1: Creates RBAC resources in the remote cluster. For this your current kube-context should point to the remote cluster.
  - Creates a `ServiceAccount` which is later used by Flux to connect to the remote cluster.
  - Creates a `ClusterRoleBinding` to bind the service account to the cluster-admin role (you can scope this down if needed).
  - Creates a `Secret` of type `kubernetes.io/service-account-token` which contains the token used to authenticate as the service account.
- Step 2: Reads token and certificate data from the created secret.
- Step 3: Creates a kubeconfig file using the token and certificate data.
- Step 4: Base64 encodes the kubeconfig file and prints it to stdout.

Copy the base64 encoded kubeconfig string to the Kubernetes secret as printed below.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kubeconfig-remote-cluster
  namespace: target-namespace
data:
  kubeconfig: <base64-encoded-kubeconfig>
```
