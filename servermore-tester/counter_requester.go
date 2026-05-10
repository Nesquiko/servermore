package main

import "net/http"

type counterRequester struct {
	rng *randWrapper
}

func newCounterRequester() Requester {
	return &counterRequester{rng: newRandWrapper()}
}

func (r *counterRequester) BinaryName() string {
	return "counter"
}

func (r *counterRequester) Description() string {
	return "Stateful counter with count, peek, reset, and slow paths."
}

func (r *counterRequester) SuggestedName() string {
	return defaultSuggestedName(r.BinaryName())
}

func (r *counterRequester) NextInvocation() InvocationSpec {
	switch r.rng.Intn(10) {
	case 0:
		return makeRequest(http.MethodGet, "/reset", "", withStandardHeaders(nil))
	case 1, 2:
		return makeRequest(http.MethodGet, "/peek", "", withStandardHeaders(nil))
	case 3:
		return makeRequest(http.MethodGet, "/slow", "", withStandardHeaders(nil))
	default:
		return makeRequest(http.MethodGet, "/count", "", withStandardHeaders(nil))
	}
}
