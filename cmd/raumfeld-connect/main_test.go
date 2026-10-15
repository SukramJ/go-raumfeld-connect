// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"slices"
	"testing"
)

func TestSplitRooms(t *testing.T) {
	t.Parallel()
	got := splitRooms(" Arbeitszimmer Melanie , Wannenbad,, ")
	if want := []string{"Arbeitszimmer Melanie", "Wannenbad"}; !slices.Equal(got, want) {
		t.Fatalf("splitRooms = %q, want %q", got, want)
	}
}
