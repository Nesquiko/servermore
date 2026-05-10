package main

import "net/http"

type helloRequester struct{}

func newHelloRequester() Requester {
	return &helloRequester{}
}

func (r *helloRequester) BinaryName() string {
	return "hello"
}

func (r *helloRequester) Description() string {
	return "Simple greeting JSON responder with a single route."
}

func (r *helloRequester) SuggestedName() string {
	return defaultSuggestedName(r.BinaryName())
}

func (r *helloRequester) NextInvocation() InvocationSpec {
	return makeRequest(http.MethodGet, "/", "", withStandardHeaders(nil))
}
