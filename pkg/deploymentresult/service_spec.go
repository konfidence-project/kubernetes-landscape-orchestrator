// Package deploymentresult defines the wire contract for deployment results that the Kubernetes deployer publishes on
// ArtifactDeployment and VectorData status and that consuming services read back from vector data. Keeping the schema
// here lets producers and consumers share a single, documented definition instead of re-declaring it.
package deploymentresult

import corev1 "k8s.io/api/core/v1"

// TypeHTTPK8sService is the DeploymentResult.Type for a Kubernetes Service exposed as a deployment result. Its Spec is
// a ServiceSpec.
const TypeHTTPK8sService = "http-k8s-service"

// ServiceSpec is the JSON payload carried in DeploymentResult.Spec for a TypeHTTPK8sService result. Consumers reach
// the Service in-cluster at <K8sName>.<Namespace>.svc.cluster.local on one of ServicePorts. Field names are
// capitalised to match the on-the-wire representation.
type ServiceSpec struct {
	// Namespace is the namespace of the Service.
	Namespace string `json:"Namespace"`
	// K8sName is the name of the Service.
	K8sName string `json:"K8sName"`
	// ServicePorts are the Service's ports.
	ServicePorts []corev1.ServicePort `json:"ServicePorts"`
}
