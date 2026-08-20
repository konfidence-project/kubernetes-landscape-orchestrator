package fluxcd

import (
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

type KustomizeResource struct {
	konfidencev1alpha1.OCMResource
	Repository string
	Path       string
	Tag        string
}
