package fluxcd

import (
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

type HelmChartResource struct {
	konfidencev1alpha1.OCMResource
	Repository string
	ChartName  string
	Version    string
}
