package ffibridge

import "testing"

// TestCallbackRoundTripIntegerArgs drives a callback through the real native
// trampoline and back via Bridge.Call — the exact path f4 plugins use. The
// unit tests around callbackTrampoline validate types and converters in
// isolation; only this round trip exercises the platform's trampoline and
// caller marshalling together, which is where Windows' uintptr-only callback
// ABI historically broke.
func TestCallbackRoundTripIntegerArgs(t *testing.T) {
	if !Supported {
		t.Skip("ffibridge: FFI is disabled in this build")
	}
	b := New(Options{})
	t.Cleanup(func() { _ = b.Close() })

	// Callback arguments arrive normalized the way fromGo hands them out:
	// every signed kind as int64, unsigned as uint64, ptr as uintptr.
	var seen []int64
	addr, err := b.NewCallback("i32(i32,i32)", func(args []any) (any, error) {
		l := args[0].(int64)
		r := args[1].(int64)
		seen = append(seen, l, r)
		return l + r, nil
	})
	if err != nil {
		t.Fatalf("NewCallback: %v", err)
	}
	if addr == 0 {
		t.Fatal("NewCallback returned a null address")
	}

	got, err := b.Call(addr, "i32(i32,i32)", 20, 22)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if v, ok := got.(int64); !ok || v != 42 {
		t.Fatalf("callback result = %#v (%T), want int64(42)", got, got)
	}

	// Negative values cross the register boundary in both directions: the
	// argument arrives truncated from a register slot, the return travels
	// back sign-extended.
	got, err = b.Call(addr, "i32(i32,i32)", -7, 5)
	if err != nil {
		t.Fatalf("Call (negative): %v", err)
	}
	if v, ok := got.(int64); !ok || v != -2 {
		t.Fatalf("callback result = %#v (%T), want int64(-2)", got, got)
	}

	want := []int64{20, 22, -7, 5}
	if len(seen) != len(want) {
		t.Fatalf("callback saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("callback saw %v, want %v", seen, want)
		}
	}
}
