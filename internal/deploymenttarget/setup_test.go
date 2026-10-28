package deploymenttarget

import (
	"context"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://example.com:6443
    insecure-skip-tls-verify: true
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: test-token
`

const (
	secretKind  = "Secret"
	landscapeNs = "landscape"
	remoteName  = "remote"
)

func TestValidate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		params    *konfidencev1alpha1.Parameters
		secret    *corev1.Secret
		wantValid bool
		wantErr   bool
	}{
		{name: "local target", params: nil, wantValid: true},
		{
			name:      "valid kubeconfig",
			params:    &konfidencev1alpha1.Parameters{Kind: secretKind, Name: remoteName},
			secret:    &corev1.Secret{Name: remoteName, Namespace: landscapeNs, Data: map[string][]byte{"value": []byte(validKubeconfig)}},
			wantValid: true,
		},
		{name: "unsupported reference", params: &konfidencev1alpha1.Parameters{Kind: "ConfigMap", Name: remoteName}, wantValid: false},
		{name: "missing secret", params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "missing"}, wantErr: true},
		{
			name:      "secret without kubeconfig",
			params:    &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "empty"},
			secret:    &corev1.Secret{Name: "empty", Namespace: landscapeNs, Data: map[string][]byte{"other": []byte("value")}},
			wantValid: false,
		},
		{
			name:   "invalid kubeconfig",
			params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "invalid"},
			secret: &corev1.Secret{
				Name:      "invalid",
				Namespace: landscapeNs,
				Data:      map[string][]byte{"value": []byte("not a kubeconfig")},
			},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			if tt.secret != nil {
				objects = append(objects, tt.secret)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			dt := &konfidencev1alpha1.DeploymentTarget{
				Namespace: landscapeNs,
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					ArtifactType: konfidencev1alpha1.MustArtifactType("test.konfidence.cloud"),
					Parameters:   tt.params,
				},
			}
			result, err := validate(context.Background(), c, dt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && result.Valid() != tt.wantValid {
				t.Fatalf("validate() valid = %v, want %v (message: %q)", result.Valid(), tt.wantValid, result.Message())
			}
		})
	}
}
