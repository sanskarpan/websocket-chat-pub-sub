package snowflake_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/websocket-chat/pkg/snowflake"
)

func TestSnowflakeGenerationUniqueness(t *testing.T) {
	gen := snowflake.New(1)
	assert.NotNil(t, gen)

	count := 1000
	idMap := sync.Map{}
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := gen.Generate()
			_, loaded := idMap.LoadOrStore(id, true)
			assert.False(t, loaded, "Duplicate snowflake ID generated: %d", id)
		}()
	}

	wg.Wait()
}
