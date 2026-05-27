package utils

import (
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SanitizeK8sResourceName makes a string DNS-1123 compatible (valid K8s resource name)
func SanitizeK8sResourceName(name string) string {
	// lowercase the name
	name = strings.ToLower(name)

	// replace any character not allowed with a dash
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	name = reg.ReplaceAllString(name, "-")

	// trim leading/trailing non-alphanumeric characters
	name = strings.TrimLeftFunc(name, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	name = strings.TrimRightFunc(name, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	// truncate to 63 characters max
	if len(name) > 63 {
		name = name[:63]
	}

	return name
}

// Must helper function that panics if the error is not nil, otherwise returns the string
func Must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

func GetKonfidenceLabel(meta *metav1.ObjectMeta, label string) (string, error) {
	value := meta.GetLabels()[fmt.Sprintf("konfidence.cloud/%s", label)]
	if value == "" {
		return "", fmt.Errorf("label %s not found in metadata", label)
	}
	return value, nil
}

// ParseHostnameWithPortFromURL extracts the hostname (incl. port) from a URL-like string
func ParseHostnameWithPortFromURL(stringUrl string) (string, error) {
	// split the string at the first "/" to separate host:port from the rest
	parts := strings.SplitN(removeProtocol(stringUrl), "/", 2)

	if len(parts) < 1 {
		return "", fmt.Errorf("invalid URL: %s", stringUrl)
	}
	return parts[0], nil
}

func removeProtocol(stringUrl string) string {
	parts := strings.SplitN(stringUrl, "//", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return stringUrl
}
