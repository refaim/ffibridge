package ffibridge

import (
	"reflect"
	"strings"
	"testing"
)

func TestCallbackTrampolineExactTypes(t *testing.T) {
	sig, err := ParseSignature("f64(i32,f32,ptr)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}

	ftype, args, result, err := callbackTrampoline(sig, false)
	if err != nil {
		t.Fatalf("callbackTrampoline: %v", err)
	}
	if ftype != sig.FuncType() {
		t.Fatalf("function type = %v, want %v", ftype, sig.FuncType())
	}

	values := []reflect.Value{
		reflect.ValueOf(int32(-7)),
		reflect.ValueOf(float32(1.5)),
		reflect.ValueOf(uintptr(0x1234)),
	}
	for i, value := range values {
		if got := args[i](value); got.Interface() != value.Interface() {
			t.Errorf("argument %d = %#v, want %#v", i+1, got.Interface(), value.Interface())
		}
	}
	wantResult := reflect.ValueOf(float64(2.5))
	if got := result(wantResult); got.Interface() != wantResult.Interface() {
		t.Errorf("result = %#v, want %#v", got.Interface(), wantResult.Interface())
	}
}

func TestCallbackTrampolineWideTypesAndConversions(t *testing.T) {
	sig, err := ParseSignature("i32(bool,i8,u8,i16,u16,i32,u32,i64,u64,ptr)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}

	ftype, args, result, err := callbackTrampoline(sig, true)
	if err != nil {
		t.Fatalf("callbackTrampoline: %v", err)
	}
	wide := reflect.TypeOf(uintptr(0))
	if ftype.NumIn() != len(sig.Args) || ftype.NumOut() != 1 || ftype.Out(0) != wide {
		t.Fatalf("function type = %v, want uintptr arguments and return", ftype)
	}
	for i := range ftype.NumIn() {
		if ftype.In(i) != wide {
			t.Errorf("argument type %d = %v, want uintptr", i+1, ftype.In(i))
		}
	}

	cases := []struct {
		raw  uintptr
		want any
	}{
		{2, true},
		{0xff, int64(-1)},
		{0xff, uint64(0xff)},
		{0xffff, int64(-1)},
		{0xffff, uint64(0xffff)},
		{0xffffffff, int64(-1)},
		{0xffffffff, uint64(0xffffffff)},
		{^uintptr(0), int64(-1)},
		{^uintptr(0), uint64(^uintptr(0))},
		{0x1234, uintptr(0x1234)},
	}
	for i, tc := range cases {
		got := fromGo(sig.Args[i], args[i](reflect.ValueOf(tc.raw)))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("argument %d from %#x = %#v, want %#v", i+1, tc.raw, got, tc.want)
		}
	}

	declared, err := toGo(KindI32, -7)
	if err != nil {
		t.Fatalf("toGo: %v", err)
	}
	wideResult := result(declared).Interface().(uintptr)
	negative := int64(-7)
	if want := uintptr(negative); wideResult != want {
		t.Errorf("wide result = %#x, want sign-extended %#x", wideResult, want)
	}
	if got := fromGo(KindI32, args[5](reflect.ValueOf(wideResult))); got != negative {
		t.Errorf("round trip = %#v, want %d", got, negative)
	}
}

func TestCallbackTrampolineWideVoid(t *testing.T) {
	sig, err := ParseSignature("void(i32)")
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	ftype, _, result, err := callbackTrampoline(sig, true)
	if err != nil {
		t.Fatalf("callbackTrampoline: %v", err)
	}
	if ftype.NumIn() != 1 || ftype.In(0).Kind() != reflect.Uintptr || ftype.NumOut() != 0 {
		t.Fatalf("function type = %v, want func(uintptr)", ftype)
	}
	if result != nil {
		t.Fatal("void trampoline has a result converter")
	}
}

func TestCallbackTrampolineWideRejectsFloats(t *testing.T) {
	for _, text := range []string{"i32(f32)", "f64()"} {
		sig, err := ParseSignature(text)
		if err != nil {
			t.Fatalf("ParseSignature(%q): %v", text, err)
		}
		if _, _, _, err := callbackTrampoline(sig, true); err == nil || !strings.Contains(err.Error(), "not supported by Windows callbacks") {
			t.Errorf("callbackTrampoline(%q) error = %v, want Windows float error", text, err)
		}
	}
}
