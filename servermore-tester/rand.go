package main

import "math/rand"

type randWrapper struct {
	rng *rand.Rand
}

func newRandWrapper() *randWrapper {
	return &randWrapper{rng: newRequesterRNG()}
}

func (r *randWrapper) Intn(max int) int {
	return r.rng.Intn(max)
}
