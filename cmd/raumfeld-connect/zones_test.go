// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "testing"

func TestZoneOwnersExclusive(t *testing.T) {
	t.Parallel()
	z := newZoneOwners()
	if _, _, ok := z.acquire("zone1", "A"); !ok {
		t.Fatal("A could not claim a free zone")
	}
	if owner, _, ok := z.acquire("zone1", "B"); ok || owner != "A" {
		t.Fatalf("B claimed A's zone: owner=%q ok=%v", owner, ok)
	}
	if _, _, ok := z.acquire("zone2", "B"); !ok {
		t.Fatal("B could not claim another zone")
	}
	woken := z.released()
	z.release("zone1", "B") // not B's: no effect
	select {
	case <-woken:
		t.Fatal("release by a non-owner woke the waiters")
	default:
	}
	z.release("zone1", "A")
	select {
	case <-woken:
	default:
		t.Fatal("release did not wake the waiters")
	}
}

func TestZoneOwnersHandOff(t *testing.T) {
	t.Parallel()
	z := newZoneOwners()
	ho := &handoff{s: &saved{volume: 27}}
	z.acquire("zone1", "A")
	if z.handOff("zone1", "A", ho) {
		t.Fatal("handed off without a waiter")
	}
	z.wait("zone1", "B")
	if !z.handOff("zone1", "A", ho) {
		t.Fatal("no hand-off to the waiting B")
	}
	_, got, ok := z.acquire("zone1", "B")
	if !ok || got != ho {
		t.Fatalf("B acquired ok=%v handoff=%v, want the handed state", ok, got)
	}
	if _, again, _ := z.acquire("zone1", "B"); again != nil {
		t.Fatal("the handed state was given out twice")
	}
}

func TestZoneOwnersOrphanedHandOff(t *testing.T) {
	t.Parallel()
	z := newZoneOwners()
	ho := &handoff{s: &saved{volume: 27}}
	z.acquire("zone1", "A")
	z.wait("zone1", "B")
	z.handOff("zone1", "A", ho)
	// B's session ends before it takes the zone: B must restore it.
	orphans := z.unwait("B")
	if len(orphans) != 1 || orphans[0] != ho {
		t.Fatalf("orphans = %v, want the handed state", orphans)
	}
	if len(z.unwait("B")) != 0 {
		t.Fatal("orphan returned twice")
	}
}
