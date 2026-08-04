# ffibridge

`ffibridge` is a lightweight, sandbox-friendly Foreign Function Interface (FFI) broker written in Go. It projects native Host C ABI functions into sandboxed runtimes (such as WebAssembly via `wazero` or embedded Lua states) using a simple, human-readable signature language.

To ensure high performance and seamless cross-platform portability without compilation obstacles, `ffibridge` is built on top of [pureffi](https://github.com/unxed/pureffi), which leverages [goffi](https://github.com/go-webgpu/goffi) for low-overhead dynamic execution. The library is entirely `cgo`-free.

---

## Why ffibridge?

1. **Sandbox Escape Hatch**: It acts as a secure, controlled bridge. Instead of writing custom bindings for every host API, a sandboxed guest describes the desired ABI signature, and the broker performs the native call.
2. **Platform Portability**: Since it does not use `cgo`, Go can compile and run binaries containing this bridge across different target operating systems and architectures without requiring a C toolchain.
3. **Decoupled Security**: Gating is separated from implementation. You can attach a single security hook (`Allow`) to intercept and validate every dynamic library load, symbol resolution, or raw memory access.

---

## Signature Mini-Language

The bridge translates C prototypes into a compact, textual signature format.
