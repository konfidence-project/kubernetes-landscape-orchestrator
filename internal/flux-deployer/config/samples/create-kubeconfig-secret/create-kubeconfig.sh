#!/usr/bin/env bash
set -euo pipefail

mkdir ../../../.kube || true
KUBECONFIG_OUT="../../../.kube/.kubeconfig"
CONTEXT="${1:-}" # Pass kube context as argument

# --- Step 1: Create service account and token ---
echo ">>> Creating service account and role binding..."
kubectl apply -f service-account.yaml --context "$CONTEXT"

# --- Step 2: Get token, API server, and CA cert ---
echo ">>> Fetching token and cluster details..."
TOKEN=$(kubectl --context "$CONTEXT" -n kube-system get secret konfidence-deployer-token -o jsonpath='{.data.token}' | base64 -d)
CA_CRT=$(kubectl --context "$CONTEXT" -n kube-system get secret konfidence-deployer-token -o jsonpath='{.data.ca\.crt}')
SERVER=$(kubectl --context "$CONTEXT" config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# --- Step 3: Build kubeconfig ---
echo ">>> Writing kubeconfig to $KUBECONFIG_OUT..."
cat > "$KUBECONFIG_OUT" <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA_CRT}
    server: ${SERVER}
  name: remote-cluster
contexts:
- context:
    cluster: remote-cluster
    user: konfidence-deployer
  name: konfidence@remote-cluster
current-context: konfidence@remote-cluster
users:
- name: konfidence-deployer
  user:
    token: ${TOKEN}
EOF

# --- Step 4: Output the kubeconfig in base64 format ---
echo ">>> Kubeconfig (base64 encoded):"
cat $KUBECONFIG_OUT | base64 -w0
