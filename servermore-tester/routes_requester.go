package main

import (
	"fmt"
	"net/http"
)

type routesRequester struct {
	rng *randWrapper
}

func newRoutesRequester() Requester {
	return &routesRequester{rng: newRandWrapper()}
}

func (r *routesRequester) BinaryName() string {
	return "routes"
}

func (r *routesRequester) Description() string {
	return "Multi-route function exercising echo, math, sleep, and teapot paths."
}

func (r *routesRequester) SuggestedName() string {
	return defaultSuggestedName(r.BinaryName())
}

func (r *routesRequester) NextInvocation() InvocationSpec {
	switch r.rng.Intn(6) {
	case 0:
		return makeRequest(http.MethodGet, "/", "", withStandardHeaders(nil))
	case 1:
		payload := fmt.Sprintf("routes-echo-%03d", r.rng.Intn(1000))
		return makeRequest(
			http.MethodPost,
			fmt.Sprintf("/echo?batch=%d", r.rng.Intn(8)+1),
			payload,
			withStandardHeaders(map[string]string{"Content-Type": "text/plain"}),
		)
	case 2:
		return makeRequest(http.MethodGet, "/sleep", "", withStandardHeaders(nil))
	case 3:
		a := r.rng.Intn(50)
		b := r.rng.Intn(50)
		return makeRequest(
			http.MethodGet,
			fmt.Sprintf("/math/add?a=%d&b=%d", a, b),
			"",
			withStandardHeaders(nil),
		)
	case 4:
		a := r.rng.Intn(12) + 1
		b := r.rng.Intn(12) + 1
		return makeRequest(
			http.MethodGet,
			fmt.Sprintf("/math/mul?a=%d&b=%d", a, b),
			"",
			withStandardHeaders(nil),
		)
	default:
		return makeRequest(http.MethodGet, "/status/teapot", "", withStandardHeaders(nil))
	}
}
