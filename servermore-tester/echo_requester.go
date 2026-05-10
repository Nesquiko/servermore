package main

import (
	"fmt"
	"net/http"
)

type echoRequester struct {
	rng *randWrapper
}

func newEchoRequester() Requester {
	return &echoRequester{rng: newRandWrapper()}
}

func (r *echoRequester) BinaryName() string {
	return "echo"
}

func (r *echoRequester) Description() string {
	return "Reflects request method, headers, and body across multiple routes."
}

func (r *echoRequester) SuggestedName() string {
	return defaultSuggestedName(r.BinaryName())
}

func (r *echoRequester) NextInvocation() InvocationSpec {
	headers := withStandardHeaders(map[string]string{"Content-Type": "text/plain"})
	switch r.rng.Intn(4) {
	case 0:
		return makeRequest(http.MethodGet, "/", "", headers)
	case 1:
		body := fmt.Sprintf("inspect-%03d", r.rng.Intn(1000))
		return makeRequest(http.MethodPost, "/inspect", body, headers)
	case 2:
		headers["X-Echo-Trace"] = fmt.Sprintf("trace-%03d", r.rng.Intn(1000))
		return makeRequest(http.MethodGet, "/headers", "", headers)
	default:
		body := fmt.Sprintf("body-%03d", r.rng.Intn(1000))
		return makeRequest(http.MethodPut, "/body", body, headers)
	}
}
