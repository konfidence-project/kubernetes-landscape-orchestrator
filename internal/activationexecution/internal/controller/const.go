package controller

const (
	// TypeGatewayRoute is the ActivationTaskExecution/Registration Spec.Type this executor dispatches on. It is the
	// activation-task type (which executor runs), distinct from the deployment-result type it consumes
	// (deploymentresult.TypeHTTPK8sService).
	TypeGatewayRoute = "gateway-route.konfidence.cloud"

	// headerStage selects the stage; the admin edge sets it, Konfidence matches on it and never sees the FQDN.
	headerStage = "x-konfidence-stage"

	// headerService optionally selects the target deployment result by name, as an alternative to the request path.
	headerService = "x-konfidence-service"

	// headerRoutingKey carries the active vector id east-west. Name/value are interim pending konfidence #169.
	headerRoutingKey = "x-konfidence-routing-key"

	// defaultGatewayName is the Gateway the HTTPRoutes attach to when the registration does not override it. The
	// namespace defaults to the landscape namespace.
	defaultGatewayName = "konfidence-router"

	// stageRouteFinalizer binds the per-stage HTTPRoute lifecycle to the Stage. Routes may live on a different
	// cluster than the Stage, so owner references cannot be used.
	stageRouteFinalizer = "konfidence.cloud/gateway-route-cleanup"

	// stageRouteNamePrefix prefixes the per-stage HTTPRoute name.
	stageRouteNamePrefix = "konfidence-stage-"

	// labelManagedBy marks the konfidence-router Gateway that Konfidence provisioned. A Gateway of that name without
	// the label belongs to the admin and is never modified.
	labelManagedBy      = "app.kubernetes.io/managed-by"
	managedByKonfidence = "konfidence"
)
