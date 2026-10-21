package controller

const (
	ocmResourceTypeHelmChart = "helmChart"
	ocmResourceTypeKustomize = "kustomize"

	// labelArtifactDeployment is injected onto every resource of a deployment via Flux CommonMetadata, so all
	// Services of an ArtifactDeployment can be listed by it.
	labelArtifactDeployment = "konfidence.cloud/artifact-deployment"

	// annotationDeploymentResult opts a Service into the vector's deployment results. Its value is the stable
	// result name consumers look up, independent of the deployed Service's name.
	annotationDeploymentResult = "konfidence.cloud/deployment-result"

	// annotationIngressExposed opts a deployment-result Service into north-south routing when set to "true".
	annotationIngressExposed = "konfidence.cloud/ingress-exposed"
)
