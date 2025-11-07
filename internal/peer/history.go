package peer

import (
	"errors"
	"sync"
	"time"
)

const (
	EventReceived string = "received"
	EventSent     string = "sent"
)

type Event struct {
	Timestamp   time.Time `json:"timestamp"`
	Direction   string    `json:"direction"`
	MessageType string    `json:"messageType"`
	PieceIndex  *uint32   `json:"pieceIndex,omitempty"`
	BlockOffset *uint32   `json:"blockOffset,omitempty"`
	PayloadSize int       `json:"payloadSize"`
}

type messageHistoryBuffer struct {
	buf      []*Event
	mut      sync.RWMutex
	capacity int
	size     int
	writePos int // position to write the next element
	readPos  int // position to read the oldest element
}

func newMessageHistoryBuffer(capacity int) *messageHistoryBuffer {
	if capacity <= 0 {
		panic("capacity must be positive")
	}

	return &messageHistoryBuffer{
		buf:      make([]*Event, capacity),
		capacity: capacity,
		writePos: 0,
		readPos:  0,
		size:     0,
	}
}

func (mh *messageHistoryBuffer) Add(event *Event) {
	mh.mut.Lock()
	defer mh.mut.Unlock()

	mh.buf[mh.writePos] = event
	mh.writePos = (mh.writePos + 1) % mh.capacity

	if mh.size < mh.capacity {
		mh.size++
	} else {
		mh.readPos = (mh.readPos + 1) % mh.capacity
	}
}

func (mh *messageHistoryBuffer) Get(batchSize int) ([]*Event, error) {
	mh.mut.RLock()
	defer mh.mut.RUnlock()

	if mh.size == 0 {
		return nil, errors.New("buffer is empty")
	}

	actualBatchSize := min(mh.size, batchSize)
	events := make([]*Event, actualBatchSize)
	pos := mh.readPos

	for i := 0; i < actualBatchSize; i++ {
		events[i] = mh.buf[pos]
		pos = (pos + 1) % mh.capacity
	}

	return events, nil
}
