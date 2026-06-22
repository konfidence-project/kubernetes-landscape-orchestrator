package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sync/singleflight"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	FeaturesConfigKey            = "features.json"
	AuthoredConfigKey            = "authored.json"
	DeploymentResultsPrefix      = "deploymentResults."
	JsonSuffix                   = ".json"
	DeploymentResultPrefixLength = len(DeploymentResultsPrefix)
	TargetingKey                 = "targetingKey"
	Features                     = "features"
	Authored                     = "authored"
	DeploymentResults            = "deploymentResults"
	TargetingMatchReason         = "TARGETING_MATCH"
	FlagNotFoundErrorCode        = "FLAG_NOT_FOUND"
	ParseErrorErrorCode          = "PARSE_ERROR"
	TargetingKeyMissingErrorCode = "TARGETING_KEY_MISSING"
	IfNoneMatchHeader            = "If-None-Match"
	ConfigMapPrefix              = "vector-data-"
)

type VectorConfigurationService struct {
	Client    kubernetes.Interface
	Cache     Cache
	Namespace string
	Port      int
	sfGroup   singleflight.Group // prevent multiple k8s requests

}

// NewConfigurationService returns an initialized configuration service instance
func NewConfigurationService(client kubernetes.Interface, cache Cache, namespace string, port int) *VectorConfigurationService {
	return &VectorConfigurationService{Client: client, Cache: cache, Namespace: namespace, Port: port}
}

// --- OFREP API Types ---

// EvaluationRequest contains the evaluation context used to resolve the flag value.
// The context must contain the field 'targetingKey' set to the value of the vectorId.
type EvaluationRequest struct {
	Context map[string]any `json:"context"`
}

// FlagResponse is the result of resolving a single flag value.
type FlagResponse struct {
	Key      string         `json:"key"`
	Value    any            `json:"value"`
	Variant  string         `json:"variant,omitempty"`
	Reason   string         `json:"reason"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ErrorResponse contains details that describe an error that occurred while resolving the flag value.
type ErrorResponse struct {
	Key          string `json:"key"`
	ErrorCode    string `json:"errorCode"`
	ErrorDetails string `json:"errorDetails,omitempty"`
}

// ErrorDetails contains error details when an internal server error occurred.
type ErrorDetails struct {
	ErrorDetails string `json:"errorDetails,omitempty"`
}

// BulkResponse is the result of resolving multiple flag values in a bulk request.
type BulkResponse struct {
	Flags []any `json:"flags"`
}

// Routes sets up the HTTP multiplexer and paths
func (v *VectorConfigurationService) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ofrep/v1/evaluate/flags/{key}", v.handleEvaluateSingle)
	mux.HandleFunc("POST /ofrep/v1/evaluate/flags", v.handleEvaluateBulk)
	return mux
}

func (v *VectorConfigurationService) Start(errChan chan error) *http.Server {
	server := &http.Server{Addr: ":" + strconv.Itoa(v.Port), Handler: v.routes()}
	go func() {
		slog.Info(fmt.Sprintf("Starting http server on %d", v.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("http server failed to start: %w", err)
		}
	}()

	return server
}

// handleEvaluateSingle is the handler function that evaluates a single feature flag for a given vectorId.
//
// POST /ofrep/v1/evaluate/flags/{key}
func (v *VectorConfigurationService) handleEvaluateSingle(w http.ResponseWriter, r *http.Request) {
	flagKey := r.PathValue("key")
	req, err := v.getEvaluationContext(r)
	if err != nil {
		v.errorResponse(w, http.StatusBadRequest, flagKey, ParseErrorErrorCode, err.Error())
		return
	}

	// vectorId must be stored in the targetingKey
	vectorId, ok := req.Context[TargetingKey].(string)
	if !ok {
		v.errorResponse(w, http.StatusBadRequest, flagKey, TargetingKeyMissingErrorCode, "missing or invalid targeting key")
		return
	}

	// get configuration from cache of config map
	config, err := v.getConfiguration(vectorId)
	if err != nil {
		v.internalServerErrorResponse(w, fmt.Sprintf("invalid configuration for vectorId '%s' and flag '%s': %v", vectorId, flagKey, err))
		return
	}

	if config == "" {
		v.notFoundResponse(w, flagKey, "")
		return
	}

	// get the flag value or the full vector configuration
	flagValue, err := v.getFlagValueOrConfiguration(vectorId, flagKey, config)
	if err != nil {
		v.internalServerErrorResponse(w, fmt.Sprintf("could not parse flag value/vector configuration for vectorId: %s and flag %s: %v", vectorId, flagKey, err))
		return
	}

	if flagValue == nil {
		v.notFoundResponse(w, flagKey, "")
		return
	}

	v.okResponse(w, FlagResponse{
		Key:    flagKey,
		Value:  flagValue,
		Reason: TargetingMatchReason,
	})
}

// handleEvaluateBulk is the handler function that evaluates all flags for a given vectorId.
// It generates an Etag for the response.
//
// POST /ofrep/v1/evaluate/flags
func (v *VectorConfigurationService) handleEvaluateBulk(w http.ResponseWriter, r *http.Request) {
	req, err := v.getEvaluationContext(r)
	if err != nil {
		v.bulkErrorResponse(w, http.StatusBadRequest, ParseErrorErrorCode, err.Error())
		return
	}

	// vectorId must be stored in the targetingKey
	vectorId, ok := req.Context[TargetingKey].(string)
	if !ok {
		v.bulkErrorResponse(w, http.StatusBadRequest, TargetingKeyMissingErrorCode, "missing or invalid targeting key")
		return
	}

	// get configuration from cache of config map
	config, err := v.getConfiguration(vectorId)
	if err != nil {
		v.internalServerErrorResponse(w, fmt.Sprintf("invalid configuration for vectorId '%s': %v", vectorId, err))
		return
	}

	response := BulkResponse{[]any{}}
	if config != "" {
		features, err := v.getAllFeatures(config)
		if err != nil {
			v.internalServerErrorResponse(w, fmt.Sprintf("invalid feature configuration for vectorId '%s': %v", vectorId, err))
			return
		}

		// parse all flag values
		keys := slices.Sorted(maps.Keys(features))
		flags := make([]any, 0, len(keys))
		for _, key := range keys {
			flagValue := v.parseFlagValue(features[key])
			if flagValue == nil {
				flags = append(flags, ErrorResponse{
					Key:       key,
					ErrorCode: FlagNotFoundErrorCode,
				})
				continue
			}

			flags = append(flags, FlagResponse{
				Key:    key,
				Value:  flagValue,
				Reason: TargetingMatchReason,
			})
		}

		response = BulkResponse{flags}
	}

	etag, err := v.generateEtag(response)
	if err != nil {
		v.internalServerErrorResponse(w, fmt.Sprintf("could not generate etag: %v", err))
		return
	}

	if r.Header.Get(IfNoneMatchHeader) == etag {
		v.notModifiedResponse(w)
		return
	}

	v.okResponseWithEtag(w, response, &etag)
}

// getConfiguration tries to resolve the vector configuration for the given vectorId either from the cache or from loading
// the associated configMap using the kubernetes client.
func (v *VectorConfigurationService) getConfiguration(vectorId string) (string, error) {
	// first check if the value is stored in the cache
	if cachedConfig, ok := v.Cache.Get(vectorId); ok {
		return cachedConfig, nil
	}

	// cache miss => use singleflight to bundle multiple requests
	config, err, _ := v.sfGroup.Do(vectorId, func() (any, error) {
		slog.Info(fmt.Sprintf("[Singleflight] Fetching configuration for vectorId %s", vectorId))
		cm, err := v.Client.CoreV1().ConfigMaps(v.Namespace).Get(context.Background(), ConfigMapPrefix+vectorId, metav1.GetOptions{})

		if err != nil {
			if statusError, ok := errors.AsType[*apierrors.StatusError](err); ok {
				if statusError.Status().Code == http.StatusNotFound {
					return "", nil
				}
			}

			return nil, err
		}

		configuration := make(map[string]any)
		deploymentResults := make(map[string]json.RawMessage)
		for key, value := range cm.Data {
			if key == FeaturesConfigKey {
				configuration[Features] = json.RawMessage(value)
				continue
			}

			if key == AuthoredConfigKey {
				configuration[Authored] = json.RawMessage(value)
				continue
			}

			if strings.HasPrefix(key, DeploymentResultsPrefix) {
				deploymentName := strings.TrimSpace(key[DeploymentResultPrefixLength:])
				if endsWithSuffix(deploymentName, JsonSuffix) {
					deploymentName = deploymentName[:len(deploymentName)-len(JsonSuffix)]
				}

				if len(deploymentName) == 0 {
					return nil, fmt.Errorf("configMap '%s' contains an invalid deploymentResult name: %s", vectorId, key)
				}

				deploymentResults[deploymentName] = json.RawMessage(value)
			}
		}

		if len(deploymentResults) > 0 {
			configuration[DeploymentResults] = deploymentResults
		}

		if len(configuration) == 0 {
			return "", nil
		}

		// combine to one json string
		combined, err := json.Marshal(configuration)
		if err != nil {
			return nil, fmt.Errorf("configMap '%s' contains invalid json", vectorId)
		}

		finalConfig := string(combined)

		// update cache
		v.Cache.Set(vectorId, finalConfig)
		return finalConfig, nil
	})

	if err != nil {
		return "", err
	}
	return config.(string), nil
}

// getFlagValueOrConfiguration returns either the full vector configuration for the given vectorId
// or the flag value for the given flagKey and vectorId.
func (v *VectorConfigurationService) getFlagValueOrConfiguration(vectorId string, flagKey string, config string) (any, error) {
	// if the flagKey matches the vectorId return the full configuration
	if strings.EqualFold(flagKey, vectorId) {
		return v.parseFlagValue(config), nil
	}

	features, err := v.getAllFeatures(config)
	if err != nil {
		return nil, err
	}

	return v.parseFlagValue(features[flagKey]), nil
}

// getAllFeatures extracts the features map from the vector configuration.
func (v *VectorConfigurationService) getAllFeatures(config string) (map[string]any, error) {
	// try to parse json to map
	var resultMap map[string]any

	decoder := json.NewDecoder(bytes.NewReader([]byte(config)))
	decoder.UseNumber()
	err := decoder.Decode(&resultMap)
	if err != nil {
		return nil, fmt.Errorf("could not parse config json")
	}

	// check if there is a features field
	featureMap, ok := resultMap[Features]
	if !ok {
		return nil, nil
	}

	// check that the features can be converted to a map
	features, ok := featureMap.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("could not parse features json")
	}

	return features, nil
}

// parseFlagValue parses a flag value. Supported types are int, float64, bool, string and json.
// Returns nil if the flag value uses an unsupported type.
func (v *VectorConfigurationService) parseFlagValue(val any) any {
	switch v := val.(type) {
	case json.Number:
		if intVal, err := v.Int64(); err == nil {
			return intVal
		} else if floatVal, err := v.Float64(); err == nil {
			return floatVal
		}

		return v.String()
	case string:
		if json.Valid([]byte(v)) {
			var parsedJSON any
			if err := json.Unmarshal([]byte(v), &parsedJSON); err == nil {
				return parsedJSON
			}
		}

		return v
	case bool, int, float64:
		return v
	default:
		return nil
	}
}

// getEvaluationContext parses the evaluation context from the request body.
func (v *VectorConfigurationService) getEvaluationContext(r *http.Request) (*EvaluationRequest, error) {
	var req EvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if jsonErr, ok := errors.AsType[*json.SyntaxError](err); ok {
			return nil, fmt.Errorf("syntax error in JSON at char %d", jsonErr.Offset)
		}

		return nil, fmt.Errorf("invalid JSON in request body: %v", err)
	}

	return &req, nil
}

// generateEtag generates a sha-1 etag for the given response body.
func (v *VectorConfigurationService) generateEtag(responseBody any) (string, error) {
	body, err := json.Marshal(responseBody)
	if err != nil {
		return "", err
	}

	hash := sha1.Sum(body)
	return fmt.Sprintf(`"%x"`, hash), nil
}

func (v *VectorConfigurationService) okResponse(w http.ResponseWriter, result any) {
	v.writeResponse(w, http.StatusOK, result)
}

func (v *VectorConfigurationService) okResponseWithEtag(w http.ResponseWriter, result any, etag *string) {
	v.writeResponseWithEtag(w, http.StatusOK, result, etag)
}

func (v *VectorConfigurationService) notFoundResponse(w http.ResponseWriter, flagKey string, msg string) {
	v.errorResponse(w, http.StatusNotFound, flagKey, FlagNotFoundErrorCode, msg)
}

func (v *VectorConfigurationService) errorResponse(w http.ResponseWriter, statusCode int, flagKey string, errorCode string, msg string) {
	v.writeResponse(w, statusCode, ErrorResponse{
		Key:          flagKey,
		ErrorCode:    errorCode,
		ErrorDetails: msg,
	})
}

func (v *VectorConfigurationService) internalServerErrorResponse(w http.ResponseWriter, msg string) {
	v.writeResponse(w, http.StatusInternalServerError, ErrorDetails{
		ErrorDetails: msg,
	})
}

func (v *VectorConfigurationService) bulkErrorResponse(w http.ResponseWriter, statusCode int, errorCode string, msg string) {
	v.writeResponse(w, statusCode, ErrorResponse{
		ErrorCode:    errorCode,
		ErrorDetails: msg,
	})
}

func (v *VectorConfigurationService) writeResponse(w http.ResponseWriter, statusCode int, response any) {
	v.writeResponseWithEtag(w, statusCode, response, nil)
}

func (v *VectorConfigurationService) writeResponseWithEtag(w http.ResponseWriter, statusCode int, response any, etag *string) {
	if etag != nil {
		w.Header().Set("ETag", *etag)
	}

	w.Header().Set("Content-Type", "application/json")
	jsonData, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(statusCode)
	_, _ = w.Write(jsonData)
}

func (v *VectorConfigurationService) notModifiedResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotModified)
}

func endsWithSuffix(str, suffix string) bool {
	if len(str) < len(suffix) {
		return false
	}

	return strings.EqualFold(str[len(str)-len(suffix):], suffix)
}
