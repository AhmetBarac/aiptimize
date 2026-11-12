package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/AhmetBarac/aiptimize/internal/store"
)

// instanceIDKey is the runtime settings key holding this installation's id.
const instanceIDKey = "runtime_instance_id"

// Instance identifies one Aiptimize installation. The id is generated once,
// stored in the database and used by LAN discovery and the companion app so a
// server can be recognised even when its address changes.
type Instance struct {
	st *store.Store

	mu sync.Mutex
	id string
}

// LoadInstance reads the persisted id, creating it on first start.
func LoadInstance(ctx context.Context, st *store.Store) (*Instance, error) {
	instance := &Instance{st: st}
	raw, err := st.GetSetting(ctx, instanceIDKey)
	if err == nil {
		var id string
		if json.Unmarshal(raw, &id) == nil && id != "" {
			instance.id = id
			return instance, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	id, err := newInstanceID()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		return nil, err
	}
	if err := st.SetSetting(ctx, instanceIDKey, encoded); err != nil {
		return nil, err
	}
	instance.id = id
	return instance, nil
}

// ID returns the stable installation id.
func (i *Instance) ID() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.id
}

// newInstanceID returns a random UUID v4.
func newInstanceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
