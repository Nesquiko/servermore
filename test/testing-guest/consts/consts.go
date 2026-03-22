package testing_guest_consts

const (
	PathOK       = "/ok"
	PathNil      = "/nil"
	PathError    = "/error"
	BodyOK       = `{"status":"ok"}`
	BodyNotFound = `{"error":"not found"}`
	HeaderJSON   = "application/json"
	ErrorMessage = "testing guest error"
)
