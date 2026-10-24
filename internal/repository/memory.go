package repository

import (
	"sync"
	"time"

	"hookrelay/internal/domain"
)

// MemoryStore хранит записи и состояние очереди под одной блокировкой.
type MemoryStore struct {
	mu            sync.RWMutex
	endpoints     map[string]domain.Endpoint
	events        map[string]domain.Event
	leases        map[string]time.Time
	versions      map[string]int64
	attempts      map[string][]domain.DeliveryAttempt
	cycles        map[string]int
	next          map[string]time.Time
	lastAttemptID int64
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		endpoints: make(map[string]domain.Endpoint),
		events:    make(map[string]domain.Event),
		leases:    make(map[string]time.Time),
		versions:  make(map[string]int64),
		attempts:  make(map[string][]domain.DeliveryAttempt),
		cycles:    make(map[string]int),
		next:      make(map[string]time.Time),
	}
}
