package reconciler

import (
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	pkgctrl "github.com/konfidence-project/konfidence/pkg/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeDeployment(name string, version, hash string) *konfidencev1alpha1.ArtifactDeployment {
	ann := map[string]string{}
	if version != "" {
		ann[pkgctrl.ArtifactVersionAnnotation] = version
	}
	if hash != "" {
		ann[pkgctrl.ArtifactHashAnnotation] = hash
	}
	return &konfidencev1alpha1.ArtifactDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: ann,
		},
	}
}

func TestBuildKustomizationNameSuffix(t *testing.T) {
	const stdHash = "ab12cd34ef" // 10-char FNV hash as produced by pkghash.Fnv(..., 10)
	tests := []struct {
		name    string
		version string
		hash    string
		want    string
	}{
		{
			name:    "version+hash fits within suffix limit",
			version: "1.2.3",
			hash:    stdHash,
			want:    "-1-2-3-ab12cd34ef",
		},
		{
			name:    "version+hash suffix exactly 36 chars (fits)",
			version: "1.2.3.4.5.6.7.8.9.10.112",
			hash:    stdHash,
			want:    "-1-2-3-4-5-6-7-8-9-10-112-ab12cd34ef",
		},
		{
			name:    "version+hash exceeds limit falls back to hash only",
			version: "this-is-a-very-long-version-string",
			hash:    stdHash,
			want:    "-ab12cd34ef",
		},
		{
			name:    "version with dots sanitized",
			version: "v1.2.3",
			hash:    "deadbeef01",
			want:    "-v1-2-3-deadbeef01",
		},
		{
			name:    "empty version falls back to hash only suffix",
			version: "",
			hash:    stdHash,
			want:    "-ab12cd34ef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := makeDeployment("some-deployment", tt.version, tt.hash)
			got := buildKustomizationNameSuffix(d)
			if got != tt.want {
				t.Errorf("buildKustomizationNameSuffix(version=%q, hash=%q) = %q, want %q",
					tt.version, tt.hash, got, tt.want)
			}
			if len(got) > maxKustomizationNameSuffixLength {
				t.Errorf("result %q length %d exceeds max %d", got, len(got), maxKustomizationNameSuffixLength)
			}
		})
	}
}
