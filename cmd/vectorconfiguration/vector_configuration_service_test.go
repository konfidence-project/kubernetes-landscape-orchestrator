package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	BulkFlagEndpoint           = "/ofrep/v1/evaluate/flags"
	SingleFlagEndpoint         = BulkFlagEndpoint + "/"
	Port                       = 4000
	VectorId                   = "vector1"
	InvalidVectorId            = "vector2"
	ContentTypeHeader          = "Content-Type"
	EtagHeader                 = "Etag"
	ApplicationJson            = "application/json"
	EnableBetaFlag             = "enableBeta"
	MaxUsersFlag               = "maxUsers"
	RatioFlag                  = "ratio"
	TitleFlag                  = "title"
	DeploymentResultsOrdersKey = DeploymentResultsPrefix + "orders-db.json"
	FeaturesConfig             = "{\"enableBeta\":true," +
		"\"maxUsers\":150," +
		"\"ratio\": 4.6," +
		"\"title\": \"TestLabel\"}"
	AuthoredConfig = "{\"database\":{" +
		"\"host\":\"mysql-service\"," +
		"\"port\":3306}}"
	DeploymentResultsConfig = "{\"result\":\"something\"}"
	VectorConfig            = "{\"" + Features + "\": " +
		FeaturesConfig +
		"," +
		"\"" + Authored + "\": " +
		AuthoredConfig +
		"," +
		"\"" + DeploymentResults + "\": {" +
		"\"orders-db\":" + DeploymentResultsConfig +
		"}}"
	Etag = "\"33aba42d196a48585044a2aefa625d42f2e55ae1\""
)

type BulkFlagResponse struct {
	Flags []FlagResponse `json:"flags"`
}

var _ = Describe("Vector Configuration Service API", func() {
	var (
		fakeClient           *fake.Clientset
		cache                Cache
		configurationService *VectorConfigurationService
		recorder             *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		cache = &InMemoryCache{store: make(map[string]string)}
		fakeClient = fake.NewSimpleClientset(getDefaultConfigMap())
		configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
		recorder = httptest.NewRecorder()
	})

	Context("When evaluating a single feature flag", func() {
		DescribeTable("returns the flag successfully", func(flagKey string, expectedValue any) {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+flagKey, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))

			response := getFlagResponse(recorder)
			Expect(response.Key).To(Equal(flagKey))
			Expect(response.Value).To(Equal(expectedValue))
			Expect(response.Reason).To(Equal(TargetingMatchReason))
		},
			Entry("with a boolean flag", EnableBetaFlag, true),
			Entry("with an int flag", MaxUsersFlag, int64(150)),
			Entry("with a float flag", RatioFlag, 4.6),
			Entry("with a string flag", TitleFlag, "TestLabel"),
		)
		It("returns full vector configuration if flag matches vectorId", func() {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+VectorId, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getFlagResponse(recorder)
			Expect(response.Key).To(Equal(VectorId))
			Expect(response.Reason).To(Equal(TargetingMatchReason))
			adaptedConfigurationResponse := adaptVectorConfigurationMap(response.Value.(map[string]any))
			var expectedResult = getVectorConfigurationAsMap()
			Expect(reflect.DeepEqual(expectedResult, adaptedConfigurationResponse)).To(BeTrue())
		})
		It("returns 404 if flag does not exist", func() {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+"invalid", getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.Key).To(Equal("invalid"))
			Expect(response.ErrorCode).To(Equal(FlagNotFoundErrorCode))
			Expect(response.ErrorDetails).To(BeEmpty())
		})
		It("returns 404 if vector configMap does not exist", func() {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+RatioFlag, getEvaluationContext(InvalidVectorId))
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.Key).To(Equal("ratio"))
			Expect(response.ErrorCode).To(Equal(FlagNotFoundErrorCode))
			Expect(response.ErrorDetails).To(BeEmpty())
		})
		It("returns 404 if vector configMap does not contain any features", func() {
			fakeClient = fake.NewSimpleClientset(getConfigMap("{}", AuthoredConfig, DeploymentResultsConfig))
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", SingleFlagEndpoint+RatioFlag, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.Key).To(Equal("ratio"))
			Expect(response.ErrorCode).To(Equal(FlagNotFoundErrorCode))
			Expect(response.ErrorDetails).To(BeEmpty())
		})
		It("returns 404 if configMap is empty", func() {
			fakeClient = fake.NewSimpleClientset(getEmptyConfigMap())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", SingleFlagEndpoint+MaxUsersFlag, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.ErrorCode).To(Equal(FlagNotFoundErrorCode))
			Expect(response.ErrorDetails).To(BeEmpty())
		})
		It("returns 400 if no targeting key has been provided", func() {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+RatioFlag, getEmptyEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.Key).To(Equal("ratio"))
			Expect(response.ErrorCode).To(Equal(TargetingKeyMissingErrorCode))
			Expect(response.ErrorDetails).To(Equal("missing or invalid targeting key"))
		})
		It("returns 400 if evaluation context contains invalid json", func() {
			request := httptest.NewRequest("POST", SingleFlagEndpoint+MaxUsersFlag, getInvalidEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.Key).To(Equal("maxUsers"))
			Expect(response.ErrorCode).To(Equal(ParseErrorErrorCode))
			Expect(response.ErrorDetails).To(ContainSubstring("invalid JSON in request body"))
		})
		It("returns 500 if configMap contains invalid json", func() {
			fakeClient = fake.NewSimpleClientset(getInvalidConfigMap())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", SingleFlagEndpoint+MaxUsersFlag, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorDetails(recorder)
			Expect(response.ErrorDetails).To(ContainSubstring("contains invalid json"))
		})
		It("returns 500 if configMap contains invalid features", func() {
			fakeClient = fake.NewSimpleClientset(getConfigMapWithInvalidFeatures())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", SingleFlagEndpoint+MaxUsersFlag, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorDetails(recorder)
			Expect(response.ErrorDetails).To(ContainSubstring("invalid configuration"))
		})
	})

	Context("When evaluating multiple flags", func() {
		It("returns successfully all flags", func() {
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			Expect(recorder.Header().Get(EtagHeader)).To(Equal(Etag))
			response := getBulkResponse(recorder)
			Expect(response.Flags).To(HaveLen(4))
			Expect(response.Flags[0].Key).To(Equal(EnableBetaFlag))
			Expect(response.Flags[0].Value).To(BeTrue())
			Expect(response.Flags[1].Key).To(Equal(MaxUsersFlag))
			Expect(response.Flags[1].Value).To(Equal(int64(150)))
			Expect(response.Flags[2].Key).To(Equal(RatioFlag))
			Expect(response.Flags[2].Value).To(Equal(4.6))
			Expect(response.Flags[3].Key).To(Equal(TitleFlag))
			Expect(response.Flags[3].Value).To(Equal("TestLabel"))
		})
		It("returns 200 if configMap is empty and does not contain a vector configuration", func() {
			fakeClient = fake.NewSimpleClientset(getEmptyConfigMap())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getBulkResponse(recorder)
			Expect(response.Flags).To(BeEmpty())
		})
		It("returns 304 not modified with matching if-none-match header", func() {
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getDefaultEvaluationContext())
			request.Header.Set(IfNoneMatchHeader, Etag)
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotModified))
		})
		It("returns 400 if evaluation context contains invalid json", func() {
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getInvalidEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.ErrorCode).To(Equal(ParseErrorErrorCode))
			Expect(response.ErrorDetails).To(ContainSubstring("invalid JSON in request body"))
		})
		It("returns 400 if no targeting key has been provided", func() {
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getEmptyEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorResponse(recorder)
			Expect(response.ErrorCode).To(Equal(TargetingKeyMissingErrorCode))
			Expect(response.ErrorDetails).To(Equal("missing or invalid targeting key"))
		})
		It("returns 500 if configMap contains invalid json", func() {
			fakeClient = fake.NewSimpleClientset(getInvalidConfigMap())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorDetails(recorder)
			Expect(response.ErrorDetails).To(ContainSubstring("contains invalid json"))
		})
		It("returns 500 if configMap contains invalid features", func() {
			fakeClient = fake.NewSimpleClientset(getConfigMapWithInvalidFeatures())
			configurationService = NewConfigurationService(fakeClient, cache, corev1.NamespaceDefault, Port)
			request := httptest.NewRequest("POST", BulkFlagEndpoint, getDefaultEvaluationContext())
			configurationService.routes().ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(recorder.Header().Get(ContentTypeHeader)).To(Equal(ApplicationJson))
			response := getErrorDetails(recorder)
			Expect(response.ErrorDetails).To(ContainSubstring("invalid configuration"))
		})
	})
})

func getDefaultConfigMap() *corev1.ConfigMap {
	return getConfigMap(FeaturesConfig, AuthoredConfig, DeploymentResultsConfig)
}

func getEmptyConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VectorId,
			Namespace: corev1.NamespaceDefault,
		},
		Data: map[string]string{},
	}
}

func getInvalidConfigMap() *corev1.ConfigMap {
	return getConfigMap("{a}", "", "")
}

func getConfigMapWithInvalidFeatures() *corev1.ConfigMap {
	return getConfigMap("{\"features\": true}", "", "")
}

func getConfigMap(features string, authored string, deploymentResults string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VectorId,
			Namespace: corev1.NamespaceDefault,
		},
		Data: map[string]string{
			FeaturesConfigKey:          features,
			AuthoredConfigKey:          authored,
			DeploymentResultsOrdersKey: deploymentResults,
		},
	}
}

func getDefaultEvaluationContext() *bytes.Buffer {
	return getEvaluationContext(VectorId)
}

func getEvaluationContext(vectorId string) *bytes.Buffer {
	return getPayloadBuffer(EvaluationRequest{
		Context: map[string]any{
			TargetingKey: vectorId,
		},
	})
}

func getEmptyEvaluationContext() *bytes.Buffer {
	return getPayloadBuffer(EvaluationRequest{
		Context: map[string]any{},
	})
}

func getInvalidEvaluationContext() *bytes.Buffer {
	return getPayloadBuffer("{}")
}

func getPayloadBuffer(payload any) *bytes.Buffer {
	jsonBytes, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	return bytes.NewBuffer(jsonBytes)
}

func getFlagResponse(recorder *httptest.ResponseRecorder) *FlagResponse {
	httpResponse := recorder.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		Expect(err).NotTo(HaveOccurred())
	}(httpResponse.Body)

	var flag *FlagResponse
	decoder := json.NewDecoder(httpResponse.Body)
	decoder.UseNumber()
	err := decoder.Decode(&flag)
	Expect(err).NotTo(HaveOccurred())

	numValue, ok := flag.Value.(json.Number)
	if ok {
		flag.Value = parseJsonNumberToType(numValue)
	}

	return flag
}

func getErrorResponse(recorder *httptest.ResponseRecorder) *ErrorResponse {
	httpResponse := recorder.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		Expect(err).NotTo(HaveOccurred())
	}(httpResponse.Body)

	var errorResponse *ErrorResponse
	err := json.NewDecoder(httpResponse.Body).Decode(&errorResponse)
	Expect(err).NotTo(HaveOccurred())
	return errorResponse
}

func getErrorDetails(recorder *httptest.ResponseRecorder) *ErrorDetails {
	httpResponse := recorder.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		Expect(err).NotTo(HaveOccurred())
	}(httpResponse.Body)

	var errorDetailsResponse *ErrorDetails
	err := json.NewDecoder(httpResponse.Body).Decode(&errorDetailsResponse)
	Expect(err).NotTo(HaveOccurred())
	return errorDetailsResponse
}

func getBulkResponse(recorder *httptest.ResponseRecorder) *BulkFlagResponse {
	httpResponse := recorder.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		Expect(err).NotTo(HaveOccurred())
	}(httpResponse.Body)

	var flags *BulkFlagResponse
	decoder := json.NewDecoder(httpResponse.Body)
	decoder.UseNumber()
	err := decoder.Decode(&flags)
	Expect(err).NotTo(HaveOccurred())

	for i, flag := range flags.Flags {
		numValue, ok := flag.Value.(json.Number)
		if ok {
			flags.Flags[i].Value = parseJsonNumberToType(numValue)
		}
	}

	return flags
}

func getVectorConfigurationAsMap() map[string]any {
	decoder := json.NewDecoder(strings.NewReader(VectorConfig))
	decoder.UseNumber()
	var configuration map[string]any
	err := decoder.Decode(&configuration)
	Expect(err).NotTo(HaveOccurred())
	configuration = adaptVectorConfigurationMap(configuration)
	return configuration
}

func adaptVectorConfigurationMap(configuration map[string]any) map[string]any {
	features, ok := configuration[Features].(map[string]any)
	Expect(ok).To(BeTrue())
	adaptMapNumValues(features)
	return configuration
}

func adaptMapNumValues(valuesMap map[string]any) map[string]any {
	for k, v := range valuesMap {
		numValue, ok := v.(json.Number)
		if ok {
			valuesMap[k] = parseJsonNumberToType(numValue)
		}
	}

	return valuesMap
}

func parseJsonNumberToType(flagValue json.Number) any {
	if intVal, err := flagValue.Int64(); err == nil {
		return intVal
	} else if floatVal, err := flagValue.Float64(); err == nil {
		return floatVal
	}

	return flagValue.String()
}
