/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSanitizeK8sResourceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple valid name",
			input:    "my-resource",
			expected: "my-resource",
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
			expected: "my-resource",
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
					"konfidence.cloud/deployment": "my-deployment",
					"konfidence.cloud/version":    "v1.0.0",
				},
			},
			label:    "deployment",
			expected: "my-deployment",
		},
		{
			name: "label does not exist",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/deployment": "my-deployment",
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
			label:    "deployment",
			expected: "",
		},
		{
			name:     "nil labels map",
			input:    &metav1.ObjectMeta{},
			label:    "deployment",
			expected: "",
		},
		{
			name: "label exists but is empty string",
			input: &metav1.ObjectMeta{
				Labels: map[string]string{
					"konfidence.cloud/deployment": "",
				},
			},
			label:    "deployment",
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
					"konfidence.cloud/Deployment": "my-deployment",
				},
			},
			label:    "deployment",
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
