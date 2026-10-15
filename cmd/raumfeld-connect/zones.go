// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "sync"

// zoneOwners lets one bridge at a time play to a zone. Rooms grouped in one
// zone share its renderer, so a second receiver of that zone must not take
// it over; it waits. When the owner's session ends while another room
// waits, the zone is handed over as it is, with the state saved before the
// first takeover, instead of being restored and taken over again: restore
// followed at once by a takeover left the zone TRANSITIONING (measured
// 2026-10-15).
type zoneOwners struct {
	mu      sync.Mutex
	owner   map[string]string          // zone UDN → room playing to it
	waiting map[string]map[string]bool // zone UDN → rooms waiting for it
	handed  map[string]*handoff        // zone UDN → state left by the last owner
	changed chan struct{}              // closed and replaced when a zone frees up
}

// handoff is a zone passed on without restoring it.
type handoff struct {
	t *target
	s *saved // the zone's state before the first takeover
}

func newZoneOwners() *zoneOwners {
	return &zoneOwners{
		owner:   map[string]string{},
		waiting: map[string]map[string]bool{},
		handed:  map[string]*handoff{},
		changed: make(chan struct{}),
	}
}

// acquire claims the zone for room. It fails with the current owner when
// another room holds it; otherwise it returns the state handed over by the
// previous owner, if any.
func (z *zoneOwners) acquire(zoneUDN, room string) (owner string, ho *handoff, ok bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if o, held := z.owner[zoneUDN]; held && o != room {
		return o, nil, false
	}
	z.owner[zoneUDN] = room
	delete(z.waiting[zoneUDN], room)
	ho = z.handed[zoneUDN]
	delete(z.handed, zoneUDN)
	return room, ho, true
}

// wait records that room waits for the zone.
func (z *zoneOwners) wait(zoneUDN, room string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.waiting[zoneUDN] == nil {
		z.waiting[zoneUDN] = map[string]bool{}
	}
	z.waiting[zoneUDN][room] = true
}

// handOff passes room's zone to a waiting room and reports whether it did;
// if nobody waits, room keeps the zone and must restore and release it.
func (z *zoneOwners) handOff(zoneUDN, room string, ho *handoff) bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.owner[zoneUDN] != room || len(z.waiting[zoneUDN]) == 0 {
		return false
	}
	delete(z.owner, zoneUDN)
	z.handed[zoneUDN] = ho
	z.wake()
	return true
}

// release gives the zone up if room holds it.
func (z *zoneOwners) release(zoneUDN, room string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.owner[zoneUDN] != room {
		return
	}
	delete(z.owner, zoneUDN)
	z.wake()
}

// unwait drops room from every wait list. A zone handed over that now has
// neither owner nor waiter is returned: the caller must restore it.
func (z *zoneOwners) unwait(room string) []*handoff {
	z.mu.Lock()
	defer z.mu.Unlock()
	var orphans []*handoff
	for zoneUDN, rooms := range z.waiting {
		if !rooms[room] {
			continue
		}
		delete(rooms, room)
		if _, owned := z.owner[zoneUDN]; !owned && len(rooms) == 0 && z.handed[zoneUDN] != nil {
			orphans = append(orphans, z.handed[zoneUDN])
			delete(z.handed, zoneUDN)
		}
	}
	return orphans
}

// released is closed the next time a zone frees up.
func (z *zoneOwners) released() <-chan struct{} {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.changed
}

func (z *zoneOwners) wake() {
	close(z.changed)
	z.changed = make(chan struct{})
}
