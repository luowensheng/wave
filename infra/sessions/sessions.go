// Package sessions provides concrete session-store adapters. Today
// only the in-memory store lives here; the SQLite session store will
// move alongside it. The package has no awareness of the auth feature
// it serves; it just stores domain.Session values.
package sessions

import (
	"github.com/luowensheng/wave/domain"
	"errors"
	"fmt"
	"sync"
	"time"
)

type InMemoryStore struct {
	data    map[string]*domain.Session
	counter int
	lock    sync.RWMutex
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		data: make(map[string]*domain.Session),
		lock: sync.RWMutex{},
	}
}

func (i *InMemoryStore) CreateSession(userID string, duration time.Duration) (*domain.Session, error) {
	i.lock.Lock()
	defer i.lock.Unlock()

	now := time.Now()
	session := &domain.Session{
		ID:        fmt.Sprintf("session_%d_%d", i.counter, now.Unix()),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(duration),
		Revoked:   false,
	}
	i.counter++
	i.data[session.ID] = session
	return session, nil
}

func (i *InMemoryStore) GetSession(sessionID string) (*domain.Session, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	session, exists := i.data[sessionID]
	if !exists {
		return nil, errors.New("session not found")
	}
	if session.Revoked {
		return nil, errors.New("session revoked")
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, errors.New("session expired")
	}
	return session, nil
}

func (i *InMemoryStore) RevokeSession(sessionID string) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	session, exists := i.data[sessionID]
	if !exists {
		return errors.New("session not found")
	}
	session.Revoked = true
	return nil
}
