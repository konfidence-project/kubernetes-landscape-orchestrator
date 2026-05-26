# Deployment Target

The Flux-Deployer supports deploying to remote clusters by configuring deployment targets. 
You can specify multiple deployment targets using ConfigMapRefs or Secrets to store the necessary kubeconfig information for the remote clusters.

User has two options to configure deployment targets:
1. via a Flux-Deployer specific ConfigMap `flux-deployer-configuration` in the `konfidence-system` namespace that contains deployment target definitions and their corresponding kubeconfig references.
2. via a convention-based approach where the target kubeconfig is stored in a Secret `konfidence-flux-remote-cluster-kubeconfig` in the landscape namespace.

> **Note**
> If no deployment target is defined for a landscape, the Flux-Deployer will attempt to use the in-cluster configuration to deploy to the same cluster where it is running.
<!-- -->
> **Note**
> The kubeconfig format is defined by Flux, not Konfidence. For details, see Flux documentation on [Kustomization](https://fluxcd.io/flux/components/kustomize/kustomizations/#kubeconfig-remote-clusters) and [Helm Release](https://fluxcd.io/flux/components/helm/helmreleases/#kubeconfig-remote-clusters) remote cluster access.

## Deployment Target with Secret

This example shows how to configure a deployment target with provided kubeconfig stored in a **Secret**. The secret must be in the same namespace as the landscape.

**Setup Steps:**

1. Create a Secret with your kubeconfig in the landscape namespace.
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: landscape-dev-kubeconfig
  namespace: landscape-dev
type: Opaque
stringData:
  # Replace the entire kubeconfig below with your remote cluster credentials
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
    - cluster:
        certificate-authority-data: LS0tLS1CRUdJTi...
        server: https://my-cluster.example.com:6443
      name: my-cluster
    contexts:
    - context:
        cluster: my-cluster
        user: my-user
      name: my-context
    current-context: my-context
    users:
    - name: my-user
      user:
        client-certificate-data: LS0tLS1CRUdJTi...
        client-key-data: LS0tLS1CRUdJTi...
```

2. Configure the deployment target in flux-deployer-configuration:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: flux-deployer-configuration
  namespace: konfidence-system
data:
  deploymentTargets: |
    [
      {
        "landscape": "landscape-dev",
        "secretRef": {
          "name": "landscape-dev-kubeconfig",
          "key": "kubeconfig"
        }
      }
    ]
```

3. Deploy an ArtifactDeployment that targets the remote cluster using the defined deployment target.
```yaml
apiVersion: landscape.konfidence.cloud/v1alpha1
kind: ArtifactDeployment
metadata:
  name: my-app
  namespace: landscape-dev
spec:
  manifest:
    type: cloud.konfidence.flux.kustomize
  component:
    name: my-app
    resources:
      - name: manifests
        type: kustomize
        content:
          type: ociArtifact
          imageReference: ghcr.io/example/my-app:latest
```

> **Note**
> - The Secret must exist in the same namespace as the landscape (landscape-dev)
> - The target cluster must have the landscape namespace created beforehand

---

## Configure Deployment Target with a ConfigMapRef

This example shows how to configure a deployment target using a **ConfigMap** for clusters that use **external authentication** (e.g., AWS EKS with IAM roles).

The Flux-Deployer uses the provider's authentication mechanism instead of embedded credentials.

**Setup Steps:**

1. Create a ConfigMap with your cluster information in the landscape namespace:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kubeconfig-landscape-dev
  namespace: landscape-dev
data:
  # Replace with your cloud provider (aws, gcp, azure, etc.)
  provider: "aws"
  # Replace with your cluster identifier (format depends on provider)
  cluster: "arn:aws:eks:eu-central-1:123456789012:cluster/my-cluster"
  # Replace with your service account name for authentication
  serviceAccountName: "apps-iam-role"
```

2. Configure the deployment target in flux-deployer-configuration:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: flux-deployer-configuration
  namespace: konfidence-system
data:
  deploymentTargets: |
    [
      {
        "landscape": "landscape-dev",
        "configMapRef": {
          "name": "kubeconfig-landscape-dev"
        }
      }
    ]
```

3. Deploy an ArtifactDeployment that targets the remote cluster using the defined deployment target.
```yaml
apiVersion: landscape.konfidence.cloud/v1alpha1
kind: ArtifactDeployment
metadata:
  name: my-app
  namespace: landscape-dev
spec:
  manifest:
    type: cloud.konfidence.flux.kustomize
  component:
    name: my-app
    resources:
      - name: manifests
        type: kustomize
        content:
          type: ociArtifact
          imageReference: ghcr.io/example/my-app:latest
```

> **Note**
> - This method uses provider-specific authentication (e.g., IAM roles, Workload Identity)
> - The ConfigMap must exist in the same namespace as the landscape
> - The target cluster must have the landscape namespace created beforehand

---

## Convention-based Deployment Target

This example shows the simplified approach where the Flux-Deployer automatically detects the kubeconfig from a **predefined Secret name** in the landscape namespace.

**Setup Steps:**

1. Create a Secret with the **exact name** `konfidence-flux-remote-cluster-kubeconfig` in the landscape namespace:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: konfidence-flux-remote-cluster-kubeconfig
  namespace: landscape-dev
type: Opaque
stringData:
  # Replace the entire kubeconfig below with your remote cluster credentials
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
    - cluster:
        certificate-authority-data: LS0tLS1CRUdJTi...
        server: https://my-cluster.example.com:6443
      name: my-cluster
    contexts:
    - context:
        cluster: my-cluster
        user: my-user
      name: my-context
    current-context: my-context
    users:
    - name: my-user
      user:
        client-certificate-data: LS0tLS1CRUdJTi...
        client-key-data: LS0tLS1CRUdJTi...
```

2. Deploy an ArtifactDeployment to the landscape namespace:
```yaml
apiVersion: landscape.konfidence.cloud/v1alpha1
kind: ArtifactDeployment
metadata:
  name: my-app
  namespace: landscape-dev
spec:
  manifest:
    type: cloud.konfidence.flux.kustomize
  component:
    name: my-app
    resources:
      - name: manifests
        type: kustomize
        content:
          type: ociArtifact
          imageReference: ghcr.io/example/my-app:latest
```

> **Note**
> - No configuration in flux-deployer-configuration needed
> - The Secret must exist in the same namespace as the landscape
> - The target cluster must have the landscape namespace created beforehand