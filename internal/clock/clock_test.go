package clock

import (
	"testing"
	"time"
)

func TestFake(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var c Clock = NewFake(start)
	if !c.Now().Equal(start) {
		t.Fatalf("Now = %v", c.Now())
	}
	f := c.(*Fake)
	f.Advance(90 * time.Minute)
	if want := start.Add(90 * time.Minute); !f.Now().Equal(want) {
		t.Fatalf("after Advance = %v, want %v", f.Now(), want)
	}
	f.Set(start)
	if !f.Now().Equal(start) {
		t.Fatal("Set failed")
	}
}

func TestReal(t *testing.T) {
	var c Clock = Real{}
	if time.Since(c.Now()) > time.Minute {
		t.Fatal("Real clock is far from now")
	}
}
