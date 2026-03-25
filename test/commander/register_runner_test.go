package commander_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TODO - only an example how a test should look like
func TestRegisterRunner(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)

	ctx := t.Context()
	_, err := client.Heartbeat(ctx, nil)
	assert.NoError(t, err)
}
