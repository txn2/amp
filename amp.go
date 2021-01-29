package amp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
)

type AdmissionReview string

const (
	AdmisionReviewValidate AdmissionReview = "validate"
	AdmisionReviewMutate   AdmissionReview = "mutate"
)

// Config configures the API
type Config struct {
	Log                    *zap.Logger
	HttpClient             *http.Client
	Cs                     *kubernetes.Clientset
	MutationEpAnnotation   string
	ValidationEpAnnotation string
}

// Api
type Api struct {
	*Config
}

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
	addToScheme(scheme)
}

func addToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
}

// PatchOperation
// see: http://jsonpatch.com/
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// NewApi
func NewApi(cfg *Config) (*Api, error) {
	a := &Api{Config: cfg}

	// default logger if none specified
	if a.Log == nil {
		zapCfg := zap.NewProductionConfig()
		logger, err := zapCfg.Build()
		if err != nil {
			fmt.Print(err.Error())
			os.Exit(1)
		}

		a.Log = logger
	}

	if a.HttpClient == nil {
		return nil, errors.New("no HttpClient specified")
	}

	if a.Cs == nil {
		return nil, errors.New("no Kubernetes Client Set specified")
	}

	return a, nil
}

// AdmissionReviewHandler
func (a *Api) AdmissionReviewHandler(admissionReview AdmissionReview) gin.HandlerFunc {
	return func(c *gin.Context) {
		rs, err := c.GetRawData()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"message": "unable to parse request body",
				"error":   err.Error(),
			})
			return
		}

		// verify the content type
		contentType := c.GetHeader("Content-Type")
		if contentType != "application/json" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("contentType=%s, expected application/json", contentType),
			})
			return
		}

		a.Log.Info("Handling AdmissionReview request", zap.Any("type", admissionReview))

		// The AdmissionReview that was sent to the web hook
		requestedAdmissionReview := admissionv1.AdmissionReview{}

		// The AdmissionReview that will be returned
		responseAdmissionReview := admissionv1.AdmissionReview{}

		deserializer := codecs.UniversalDeserializer()
		if _, _, err := deserializer.Decode(rs, nil, &requestedAdmissionReview); err != nil {
			a.Log.Error("decode error", zap.Error(err))
			responseAdmissionReview.Response = toAdmissionResponse(err)
		} else {
			// mutate
			if admissionReview == AdmisionReviewMutate {
				responseAdmissionReview.Response = a.mutatePod(requestedAdmissionReview)
			}

			// validate
			if admissionReview == AdmisionReviewValidate {
				responseAdmissionReview.Response = a.validatePod(requestedAdmissionReview)
			}
		}

		// Return the same UID
		responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID
		responseAdmissionReview.Kind = "AdmissionReview"
		responseAdmissionReview.APIVersion = "admission.k8s.io/v1"

		a.Log.Info("Returning response to Kubernetes")
		a.Log.Debug("Response debugging, responseAdmissionReview", zap.ByteString("value", responseAdmissionReview.Response.Patch))

		c.JSON(http.StatusOK, responseAdmissionReview)
	}
}

// validatePod
func (a *Api) validatePod(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	a.Log.Info("started validatePod admission review",
		zap.Bool("DryRun", *ar.Request.DryRun),
		zap.String("Namespace", ar.Request.Namespace))

	logInfo := []zap.Field{
		zap.String("namespace", ar.Request.Namespace),
		zap.String("annotation", a.MutationEpAnnotation),
	}

	reviewResponse := admissionv1.AdmissionResponse{}

	// amp is for pods
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		a.Log.Error("unexpected resource",
			append(logInfo,
				zap.String("expected", podResource.Resource),
				zap.String("received", ar.Request.Resource.Resource),
			)...,
		)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		a.Log.Error("deserializer failure", zap.Error(err))
		return &reviewResponse
	}
	logInfo = append(logInfo, zap.String("Pod", pod.Name))

	a.Log.Info("Pod for validation review",
		append(logInfo,
			zap.Any("PodLabels", pod.Labels),
			zap.Any("PodAnnotations", pod.Annotations),
			zap.Any("PodNamespace", pod.Namespace),
		)...,
	)

	ns, err := a.Cs.CoreV1().Namespaces().Get(ar.Request.Namespace, metav1.GetOptions{})
	if err != nil {
		a.Log.Error("unable to get namespace",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	// lookup endpoint by namespace annotation
	annotations := ns.GetAnnotations()
	ep, ok := annotations[a.ValidationEpAnnotation]
	if ok == false {
		a.Log.Warn("DEFAULT ALLOW if no validation endpoint is configured for namespace.", logInfo...)
		reviewResponse.Allowed = true
		return &reviewResponse
	}

	logInfo = append(logInfo,
		zap.String("endpoint", ep),
		zap.String("annotation", a.ValidationEpAnnotation),
	)

	a.Log.Info("got validation endpoint from namespace", logInfo...)

	body, err := json.Marshal(pod)
	if err != nil {
		a.Log.Info("unable to marshal pod",
			append(logInfo, zap.Error(err))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod is unable to Marshal pod: %s", err.Error()),
		}
		return &reviewResponse
	}

	req, err := http.NewRequest("POST", ep, bytes.NewBuffer(body))
	if err != nil {
		a.Log.Error("Unable to build NewRequest",
			append(logInfo, zap.Error(err))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod is unable to build NewRequest: %s", err.Error()),
		}
		return &reviewResponse
	}

	resp, err := a.HttpClient.Do(req)
	if err != nil {
		a.Log.Error("Unable make endpoint request",
			append(logInfo, zap.Error(err))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod is unable make endpoint request: %s", err.Error()),
		}
		return &reviewResponse
	}

	if resp.StatusCode != http.StatusOK {
		a.Log.Error("Endpoint request returned non-200 response",
			append(logInfo, zap.Int("http_status_code", resp.StatusCode))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod endpoint returned non-200, got: %v", resp.StatusCode),
		}
		return &reviewResponse
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		a.Log.Error("Error reading response body",
			append(logInfo, zap.Error(err))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod is unable to read endpoint response body: %s", err.Error()),
		}
		return &reviewResponse
	}
	_ = resp.Body.Close()

	// unmarshal response body into admissionv1.AdmissionResponse
	err = json.Unmarshal(respBody, &reviewResponse)
	if err != nil {
		a.Log.Error("unable to unmarshal response body into admissionv1.AdmissionResponse",
			append(logInfo, zap.Error(err))...,
		)
		reviewResponse.Allowed = false
		reviewResponse.Result = &metav1.Status{
			Code:    500,
			Message: fmt.Sprintf("validatePod is unable to unmarshal response body into admissionv1.AdmissionResponse: %s", err.Error()),
		}
		return &reviewResponse
	}

	return &reviewResponse
}

// mutatePod
func (a *Api) mutatePod(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	a.Log.Info("started mutatePod admission review",
		zap.Bool("DryRun", *ar.Request.DryRun),
		zap.String("Namespace", ar.Request.Namespace))

	logInfo := []zap.Field{
		zap.String("namespace", ar.Request.Namespace),
		zap.String("annotation", a.MutationEpAnnotation),
	}

	reviewResponse := admissionv1.AdmissionResponse{}
	// always allow, mutation happens first, validation can deny if it needs to
	reviewResponse.Allowed = true

	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		a.Log.Error("unexpected resource",
			append(logInfo,
				zap.String("expected", podResource.Resource),
				zap.String("received", ar.Request.Resource.Resource),
			)...,
		)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		a.Log.Error("deserializer failure", zap.Error(err))
		return &reviewResponse
	}
	logInfo = append(logInfo, zap.String("Pod", pod.Name))

	a.Log.Info("Pod for mutation review",
		append(logInfo,
			zap.Any("PodLabels", pod.Labels),
			zap.Any("PodAnnotations", pod.Annotations),
			zap.Any("PodNamespace", pod.Namespace),
		)...,
	)

	ns, err := a.Cs.CoreV1().Namespaces().Get(ar.Request.Namespace, metav1.GetOptions{})
	if err != nil {
		a.Log.Error("unable to get namespace",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	// lookup endpoint by namespace annotation
	annotations := ns.GetAnnotations()
	ep, ok := annotations[a.MutationEpAnnotation]
	if ok == false {
		a.Log.Warn("no endpoint configured for namespace", logInfo...)
		return &reviewResponse
	}

	logInfo = append(logInfo,
		zap.String("endpoint", ep),
		zap.String("annotation", a.MutationEpAnnotation),
	)

	a.Log.Info("got endpoint from namespace", logInfo...)

	body, err := json.Marshal(pod)
	if err != nil {
		a.Log.Info("unable to marshal pod",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	req, err := http.NewRequest("POST", ep, bytes.NewBuffer(body))
	if err != nil {
		a.Log.Error("Unable to build NewRequest",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	resp, err := a.HttpClient.Do(req)
	if err != nil {
		a.Log.Error("Unable make endpoint request",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	if resp.StatusCode != http.StatusOK {
		a.Log.Error("Endpoint request returned non-200 response",
			append(logInfo, zap.Int("http_status_code", resp.StatusCode))...,
		)
		return &reviewResponse
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		a.Log.Error("Error reading response body",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}
	_ = resp.Body.Close()

	var po []PatchOperation

	// example patch operation
	// see: http://jsonpatch.com/
	//
	//po := []PatchOperation{
	//	{
	//		Op:   "add",
	//		Path: "/spec/initContainers",
	//		Value: corev1.Container{
	//			Name:  "added-init-container",
	//			Image: "alpine:3.12.0",
	//		},
	//	},
	//}

	// Ensure that the response body is a PatchOperation
	// TODO: Validate PatchOperation
	err = json.Unmarshal(respBody, &po)
	if err != nil {
		a.Log.Error("Error unmarshaling response body into PatchOperation",
			append(logInfo, zap.Error(err))...,
		)
		return &reviewResponse
	}

	reviewResponse.Patch = respBody
	pt := admissionv1.PatchTypeJSONPatch
	reviewResponse.PatchType = &pt

	return &reviewResponse
}

// toAdmissionResponse is a helper function to create an AdmissionResponse
// with an embedded error see:
// https://github.com/kubernetes/kubernetes/tree/v1.15.0/test/images/webhook
func toAdmissionResponse(err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// OkHandler
func (a *Api) OkHandler(version string, mode string, service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": version, "mode": mode, "service": service})
	}
}
