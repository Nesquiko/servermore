package servermoretester

import (
	"fmt"
	"maps"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"
)

type InvocationSpec struct {
	Method  string
	Path    string
	Body    []byte
	Headers map[string]string
}

type Requester interface {
	BinaryName() string
	Description() string
	SuggestedName() string
	NextInvocation() InvocationSpec
}

var rngCounter atomic.Int64

func catalog() []Requester {
	return []Requester{
		newHelloRequester(),
		newRoutesRequester(),
		newEchoRequester(),
		newExternalRequester(),
		newCounterRequester(),
	}
}

func newRequesterRNG() *rand.Rand {
	seed := time.Now().UnixNano() + rngCounter.Add(1)
	return rand.New(rand.NewSource(seed))
}

func defaultSuggestedName(binaryName string) string {
	return fmt.Sprintf("%s-demo", binaryName)
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(headers))
	maps.Copy(cloned, headers)
	return cloned
}

func makeRequest(
	method string,
	path string,
	body string,
	headers map[string]string,
) InvocationSpec {
	return InvocationSpec{
		Method:  method,
		Path:    path,
		Body:    []byte(body),
		Headers: cloneHeaders(headers),
	}
}

func withStandardHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		headers = make(map[string]string, 2)
	}
	headers["X-Servermore-Tester"] = "true"
	return headers
}

func ensureMethod(method string) string {
	if method == "" {
		return http.MethodGet
	}
	return method
}
