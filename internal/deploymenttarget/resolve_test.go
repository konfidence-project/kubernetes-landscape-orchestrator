package deploymenttarget

import (
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetesclient"
	"k8s.io/apimachinery/pkg/types"
)

func TestResolve(t *testing.T) {
	const configName = "config"
	for _, tt := range []struct {
		name    string
		missing bool
		params  *konfidencev1alpha1.Parameters
		want    kubernetesclient.Destination
		err     bool
	}{
		{name: "missing target", missing: true, err: true},
		{name: "local", want: kubernetesclient.Destination{Local: true}},
		{name: "remote", params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: configName},
			want: kubernetesclient.Destination{SecretRef: types.NamespacedName{Namespace: "target-ns", Name: configName}}},
		{name: "non-core group", params: &konfidencev1alpha1.Parameters{APIGroup: "example.com", Kind: secretKind, Name: configName}, err: true},
		{name: "wrong kind", params: &konfidencev1alpha1.Parameters{Kind: "ConfigMap", Name: configName}, err: true},
		{name: "empty name", params: &konfidencev1alpha1.Parameters{Kind: secretKind}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var target *konfidencev1alpha1.DeploymentTarget
			if !tt.missing {
				target = &konfidencev1alpha1.DeploymentTarget{
					Namespace: "target-ns",
					Spec: konfidencev1alpha1.DeploymentTargetSpec{
						Parameters: tt.params,
					},
				}
			}
			got, err := Resolve(target)
			if got != tt.want || (err != nil) != tt.err {
				t.Fatalf("Resolve() = (%+v, %v), want (%+v, error=%v)", got, err, tt.want, tt.err)
			}
		})
	}
}
