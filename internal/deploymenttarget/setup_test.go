package deploymenttarget

import (
	"context"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		name    string
		params  *konfidencev1alpha1.Parameters
		secret  *corev1.Secret
		wantErr bool
	}{
		{name: "local target", params: nil},
		{
			name:   "valid kubeconfig",
			params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: remoteName},
			secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: remoteName, Namespace: landscapeNs}, Data: map[string][]byte{"value": []byte(validKubeconfig)}},
		},
		{name: "unsupported reference", params: &konfidencev1alpha1.Parameters{Kind: "ConfigMap", Name: remoteName}, wantErr: true},
		{name: "missing secret", params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "missing"}, wantErr: true},
		{
			name:    "secret without kubeconfig",
			params:  &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "empty"},
			secret:  &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: landscapeNs}, Data: map[string][]byte{"other": []byte("value")}},
			wantErr: true,
		},
		{
			name:   "invalid kubeconfig",
			params: &konfidencev1alpha1.Parameters{Kind: secretKind, Name: "invalid"},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: landscapeNs},
				Data:       map[string][]byte{"value": []byte("not a kubeconfig")},
			},
			wantErr: true,
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
				ObjectMeta: metav1.ObjectMeta{Namespace: landscapeNs},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					ArtifactType: konfidencev1alpha1.MustArtifactType("test.konfidence.cloud"),
					Parameters:   tt.params,
				},
			}
			err := validate(context.Background(), c, dt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
