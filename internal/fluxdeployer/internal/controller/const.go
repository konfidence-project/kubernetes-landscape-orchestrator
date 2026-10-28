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

	// annotationExposePort selects one named TCP port of a deployment-result Service for north-south routing.
	annotationExposePort = "konfidence.cloud/expose-port"

	// TODO: Replace these local reasons with the shared Konfidence API constants once a module revision exporting them
	// is available to KLO.
	artifactDeploymentStalledReasonExposePortNotFound            = "ExposePortNotFound"
	artifactDeploymentStalledReasonExposePortUnsupportedProtocol = "ExposePortUnsupportedProtocol"
)
