package testing_guest_consts

import "time"

const (
	PathOK           = "/ok"
	PathDelayed      = "/delayed"
	PathDelayedDelay = 25 * time.Millisecond
	PathNil          = "/nil"
	PathError        = "/error"
	BodyOK           = `{"status":"ok"}`
	BodyNotFound     = `{"error":"not found"}`
	HeaderJSON       = "application/json"
	ErrorMessage     = "testing guest error"
)
