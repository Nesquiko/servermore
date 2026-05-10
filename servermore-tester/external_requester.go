package servermoretester

import "net/http"

type externalRequester struct {
	rng *randWrapper
}

func newExternalRequester() Requester {
	return &externalRequester{rng: newRandWrapper()}
}

func (r *externalRequester) BinaryName() string {
	return "external"
}

func (r *externalRequester) Description() string {
	return "Checks health or fetches a todo from the configured upstream API."
}

func (r *externalRequester) SuggestedName() string {
	return defaultSuggestedName(r.BinaryName())
}

func (r *externalRequester) NextInvocation() InvocationSpec {
	if r.rng.Intn(4) == 0 {
		return makeRequest(http.MethodGet, "/health", "", withStandardHeaders(nil))
	}
	return makeRequest(http.MethodGet, "/todo", "", withStandardHeaders(nil))
}
