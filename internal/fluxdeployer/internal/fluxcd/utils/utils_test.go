package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	myResource            = "my-resource"
	exampleComWithPort    = "example.com:8080"
	exampleCom            = "example.com"
	konfidenceDeployLabel = "konfidence.cloud/deployment"
	deploymentLabel       = "deployment"
	myDeployment          = "my-deployment"
)

func TestSanitizeK8sResourceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple valid name",
			input:    myResource,
			expected: myResource,
		},
		{
			name:     "uppercase letters",
			input:    "MyResource",
			expected: "myresource",
		},
		{
			name:     "special characters",
			input:    "my_resource@example.com",
			expected: "my-resource-example-com",
		},
		{
			name:     "leading and trailing invalid chars",
			input:    "_my-resource_",
			expected: myResource,
		},
		{
			name:     "name too long",
			input:    "this-is-a-very-long-name-that-exceeds-sixty-three-characters-limit",
			expected: "this-is-a-very-long-name-that-exceeds-sixty-three-characters-li",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "multiple invalid characters",
			input:    "a@#b$%c",
			expected: "a--b--c",
		},
		{
			name:     "only invalid characters",
			input:    "@#$%^&*()",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeK8sResourceName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeK8sResourceName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseHostnameWithPortFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "hostname with protocol and path",
			input:    "https://stefanprodan.github.io/podinfo",
			expected: "stefanprodan.github.io",
		},
		{
			name:     "hostname with port and path",
			input:    "example.com:8080/path/to/resource",
			expected: exampleComWithPort,
		},
		{
			name:     "hostname without port",
			input:    "example.com/path/to/resource",
			expected: exampleCom,
		},
		{
			name:     "hostname with port only",
			input:    exampleComWithPort,
			expected: exampleComWithPort,
		},
		{
			name:     "hostname only",
			input:    exampleCom,
			expected: exampleCom,
		},
		{
			name:     "localhost with port",
			input:    "localhost:3000/api/v1",
			expected: "localhost:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := ParseHostnameWithPortFromURL(tt.input)
			if result != tt.expected {
				t.Errorf("ParseHostnameWithPortFromURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetKonfidenceLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    *metav1.ObjectMeta
		label    string
		expected string
	}{
		{
			name: "valid label exists",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					konfidenceDeployLabel:      myDeployment,
					"konfidence.cloud/version": "v1.0.0",
				},
			},
			label:    deploymentLabel,
			expected: myDeployment,
		},
		{
			name: "label does not exist",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					konfidenceDeployLabel: myDeployment,
				},
			},
			label:    "environment",
			expected: "",
		},
		{
			name: "empty labels map",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{},
			},
			label:    deploymentLabel,
			expected: "",
		},
		{
			name:     "nil labels map",
			input:    &metav1.ObjectMeta{},
			label:    deploymentLabel,
			expected: "",
		},
		{
			name: "label exists but is empty string",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					konfidenceDeployLabel: "",
				},
			},
			label:    deploymentLabel,
			expected: "",
		},
		{
			name: "label with special characters",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/app-name":  "my-app-123",
					"konfidence.cloud/namespace": "production-env",
				},
			},
			label:    "app-name",
			expected: "my-app-123",
		},
		{
			name: "label with complex value",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/resource": "deployment/nginx-ingress/v1.2.3",
				},
			},
			label:    "resource",
			expected: "deployment/nginx-ingress/v1.2.3",
		},
		{
			name: "case sensitive label matching",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/Deployment": myDeployment,
				},
			},
			label:    deploymentLabel,
			expected: "",
		},
		{
			name: "multiple konfidence labels",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/environment": "staging",
					"konfidence.cloud/version":     "v3.0.0",
					"kubernetes.io/managed-by":     "flux",
				},
			},
			label:    "environment",
			expected: "staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := GetKonfidenceLabel(tt.input, tt.label)
			if result != tt.expected {
				t.Errorf("GetKonfidenceLabel(%q, %q) = %q, want %q", tt.input, tt.label, result, tt.expected)
			}
		})
	}

}
