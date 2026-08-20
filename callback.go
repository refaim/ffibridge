package ffibridge

import (
	"errors"
	"fmt"
	"reflect"
)

// Callback is the sandbox-facing shape of a native callback body. Arguments
// arrive normalised the same way call results are, and the returned value is
// converted back according to the signature's return type.
type Callback func(args []any) (any, error)

// NewCallback builds a native function pointer that dispatches into fn. The
// address stays valid for the lifetime of the bridge; there is no portable way
// to revoke a trampoline, so callbacks are never reclaimed early.
func (b *Bridge) NewCallback(sig string, fn Callback) (uintptr, error) {
	parsed, err := ParseSignature(sig)
	if err != nil {
		return 0, err
	}
	if err := b.allow(OpCallback, parsed.Text); err != nil {
		return 0, err
	}
	if !Supported {
		return 0, ErrUnsupported
	}
	if parsed.Variadic {
		return 0, errors.New("ffibridge: variadic callbacks are not supported")
	}
	if fn == nil {
		return 0, errors.New("ffibridge: nil callback body")
	}
	if b.isClosed() {
		return 0, ErrClosed
	}

	ftype, argConverters, retConverter, err := callbackTrampoline(parsed, callbackNeedsWideArgs)
	if err != nil {
		return 0, err
	}
	impl := reflect.MakeFunc(ftype, func(in []reflect.Value) []reflect.Value {
		args := make([]any, len(in))
		for i, v := range in {
			args[i] = fromGo(parsed.Args[i], argConverters[i](v))
		}

		out, callErr := invokeCallback(fn, args)
		if parsed.Ret == KindVoid {
			return nil
		}
		if callErr != nil {
			return []reflect.Value{retConverter(reflect.Zero(parsed.Ret.reflectType()))}
		}
		converted, convErr := toGo(parsed.Ret, out)
		if convErr != nil {
			return []reflect.Value{retConverter(reflect.Zero(parsed.Ret.reflectType()))}
		}
		return []reflect.Value{retConverter(converted)}
	})

	body := impl.Interface()
	addr, err := makeCallback(body)
	if err != nil {
		return 0, err
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, ErrClosed
	}
	b.callbacks[addr] = body
	b.mu.Unlock()
	return addr, nil
}

type callbackValueConverter func(reflect.Value) reflect.Value

// callbackTrampoline describes the Go function handed to the native callback
// implementation. Windows' syscall callback machinery exposes only integer
// register-sized slots, while other implementations preserve the declared Go
// types and therefore need no conversion.
func callbackTrampoline(sig *Signature, wide bool) (reflect.Type, []callbackValueConverter, callbackValueConverter, error) {
	argConverters := make([]callbackValueConverter, len(sig.Args))
	if !wide {
		for i := range argConverters {
			argConverters[i] = callbackIdentity
		}
		if sig.Ret == KindVoid {
			return sig.ftype, argConverters, nil, nil
		}
		return sig.ftype, argConverters, callbackIdentity, nil
	}

	wideType := reflect.TypeOf(uintptr(0))
	in := make([]reflect.Type, len(sig.Args))
	for i, kind := range sig.Args {
		converter, err := wideCallbackArgument(kind)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("ffibridge: callback argument %d: %w", i+1, err)
		}
		in[i] = wideType
		argConverters[i] = converter
	}

	var out []reflect.Type
	var retConverter callbackValueConverter
	if sig.Ret != KindVoid {
		var err error
		retConverter, err = wideCallbackResult(sig.Ret)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("ffibridge: callback return: %w", err)
		}
		out = []reflect.Type{wideType}
	}
	return reflect.FuncOf(in, out, false), argConverters, retConverter, nil
}

func callbackIdentity(v reflect.Value) reflect.Value { return v }

func wideCallbackArgument(kind Kind) (callbackValueConverter, error) {
	switch kind {
	case KindBool:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(v.Uint() != 0) }, nil
	case KindI8:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(int8(v.Uint())) }, nil
	case KindU8:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uint8(v.Uint())) }, nil
	case KindI16:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(int16(v.Uint())) }, nil
	case KindU16:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uint16(v.Uint())) }, nil
	case KindI32:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(int32(v.Uint())) }, nil
	case KindU32:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uint32(v.Uint())) }, nil
	case KindI64:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(int64(v.Uint())) }, nil
	case KindU64:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uint64(v.Uint())) }, nil
	case KindPtr:
		return callbackIdentity, nil
	case KindF32, KindF64:
		return nil, fmt.Errorf("%s is not supported by Windows callbacks", kind)
	case KindStr:
		return nil, errors.New("str is not supported by Windows callbacks")
	default:
		return nil, fmt.Errorf("%s is not a callback argument type", kind)
	}
}

func wideCallbackResult(kind Kind) (callbackValueConverter, error) {
	switch kind {
	case KindBool:
		return func(v reflect.Value) reflect.Value {
			if v.Bool() {
				return reflect.ValueOf(uintptr(1))
			}
			return reflect.ValueOf(uintptr(0))
		}, nil
	case KindI8, KindI16, KindI32, KindI64:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uintptr(v.Int())) }, nil
	case KindU8, KindU16, KindU32, KindU64, KindPtr:
		return func(v reflect.Value) reflect.Value { return reflect.ValueOf(uintptr(v.Uint())) }, nil
	case KindF32, KindF64:
		return nil, fmt.Errorf("%s is not supported by Windows callbacks", kind)
	case KindStr:
		return nil, errors.New("str is not supported by Windows callbacks")
	default:
		return nil, fmt.Errorf("%s is not a callback return type", kind)
	}
}

// invokeCallback isolates the guest body: a panic inside a sandbox must not
// unwind through native frames, so it is turned into an error and the callback
// returns a zero value instead.
func invokeCallback(fn Callback, args []any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("ffibridge: callback panicked: %v", r)
		}
	}()
	return fn(args)
}
