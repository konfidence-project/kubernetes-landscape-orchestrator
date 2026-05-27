

# landscape-flux-deployer

## Requirements and Setup

### Repository Authentication Setup

When creating repository resources (e.g OCIRepository or HelmRepository) the Flux Deployer needs a (optional) secretRef with the name of the secret that contains authentication credentials to access a repository.
First the controller extracts the domain name from the registry url and then tries to lookup a Secret reference by domain name in a controller specific ConfigMap.
If a matching reference is found the the name is used in the secretRef. If the ConfigMap does not exist or no matching entry has been found
the controller uses the domain name as secretRef name. 

So there are two options to configure the repository credentials:
1. A Flux Deployer Controller specific ConfigMap `flux-deployer-configuration` in the `konfidence-system` namespace that contains one or more Secret references by domain name.
2. A Secret with the domain as name that exists in the namespace of the reconciled resource

> **Note:**
> At the moment the secret has to be of type `kubernetes.io/dockerconfigjson`

#### Example with config map
1. Create a Secret with the credentials of type `kubernetes.io/dockerconfigjson`. The Secret must be in the same namespace as the reconciled resource.
```yaml
apiVersion: v1
data:
  .dockerconfigjson: eyJhdXRocyI6eyJyZWdpc3RyeS5leGFtcGxlLmNvbSI6eyJ1c2VybmFtZSI6InRlc3QiLCJwYXNzd29yZCI6InRlc3QiLCJlbWFpbCI6InRlc3RAc2FwLmNvbSIsImF1dGgiOiJkR1Z6ZERwMFpYTjAifX19
kind: Secret
metadata:
  creationTimestamp: "2026-01-05T11:36:58Z"
  name: my-secret-123
  namespace: default
  resourceVersion: "1766473"
  uid: c17e1c74-e10b-4d1f-99a3-0a3da7f4b5a8
type: kubernetes.io/dockerconfigjson
```

2. Create the ConfigMap referencing the Secret by domain name. The ConfigMap must be in the `konfidence-system` namespace.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: flux-deployer-configuration
  namespace: konfidence-system
data:
  authenticationSecretRefs: |
    registry.example.com: my-secret-123
```