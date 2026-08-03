package controller

//go:generate go run go.uber.org/mock/mockgen -destination=./mocks/mock_status_updater.go -package=mocks github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/controller StatusUpdater

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

// StatusUpdater interface defines global functions used to maintain the status of a resource.
type StatusUpdater interface {
	MutateStatus(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error
}
