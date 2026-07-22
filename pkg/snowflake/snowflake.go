package snowflake

import (
	"errors"
	"strconv"
	"sync"
	"time"
)

const (
	epoch          = int64(1609459200000)
	timestampBits  = uint(41)
	nodeIDBits     = uint(10)
	sequenceBits   = uint(12)
	timestampShift = nodeIDBits + sequenceBits
	nodeIDShift    = sequenceBits
	sequenceMask   = int64(-1) ^ (int64(-1) << sequenceBits)
	maxNodeID      = int64(-1) ^ (int64(-1) << nodeIDBits)
)

type Generator struct {
	mu       sync.Mutex
	nodeID   int64
	sequence int64
	lastTime int64
}

func (g *Generator) Generate() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UnixNano() / 1000000
	if now < g.lastTime {
		now = g.lastTime
	}
	if now == g.lastTime {
		g.sequence = (g.sequence + 1) & sequenceMask
		if g.sequence == 0 {
			now = g.waitNextMillis(now)
		}
	} else {
		g.sequence = 0
	}

	g.lastTime = now
	id := ((now - epoch) << timestampShift) |
		(g.nodeID << nodeIDShift) |
		g.sequence
	return id
}

func (g *Generator) waitNextMillis(currentTime int64) int64 {
	for currentTime == g.lastTime {
		currentTime = time.Now().UnixNano() / 1000000
	}
	return currentTime
}

func (g *Generator) String() string {
	return strconv.FormatInt(g.Generate(), 10)
}

var defaultGenerator *Generator

func init() {
	defaultGenerator = New(1)
}

func Generate() *Generator {
	return defaultGenerator
}

func SetNodeID(nodeID int64) error {
	if nodeID < 0 || nodeID > maxNodeID {
		return errors.New("node ID must be between 0 and 1023")
	}
	defaultGenerator.nodeID = nodeID
	return nil
}

func New(nodeID int64) *Generator {
	if nodeID < 0 || nodeID > maxNodeID {
		panic("node ID must be between 0 and 1023")
	}
	return &Generator{
		nodeID:   nodeID,
		sequence: 0,
		lastTime: 0,
	}
}
