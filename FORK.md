# Fork notes

Fork of [fastschema/qjs](https://github.com/fastschema/qjs) v0.0.6 (MIT), used by agentkit's `ext/codemode`.

The module path is `github.com/RongTsai4TGM/qjs`, not the upstream one. A `replace` directive in a library's go.mod is
ignored by the programs that import that library, so a renamed module is the only way agentkit's users get these fixes.

## Changes from upstream

| Change | Why |
|---|---|
| `Runtime.Close` also closes the per-runtime wazero runtime | `New` creates one per `Runtime` (with the WASI and `env` host modules) and upstream never closed it: about 23 KB of host memory leaked per runtime. `TestCloseReleasesEverything` |
| `Runtime.Close` releases the wasm side even when freeing the QuickJS runtime panics | After the context ends, wazero closes the guest module (`CloseOnContextDone`) and freeing the QuickJS runtime panics; upstream then skipped everything after it, so every aborted or timed-out runtime leaked (about 29 KB). The panic still reaches the caller. `TestCloseAfterContextDoneReleasesEverything` |
| The process-wide compiled module is keyed on `CloseOnContextDone` too | It is a compile-time setting in wazero; upstream kept whatever the first `New` in the process asked for, so a later runtime that needed it could not be aborted |
| `Option.MaxMemoryPages` | Hard cap on the wasm linear memory (wazero `WithMemoryLimitPages`). Upstream's `MemoryLimit` relies on QuickJS's own accounting, which misses large allocations in the wasm build, so a script could grow host memory up to the 4 GiB wasm32 limit. With the cap, the script gets a catchable `InternalError: out of memory` and the runtime stays usable. `TestMaxMemoryPagesBoundsStrings` |

## Rebuilt `qjs.wasm`

`qjs.wasm` is built from the same quickjs-ng commit as upstream (`qjswasm/quickjs` submodule, d01ca44) with two patches
in `qjswasm/patches/`, applied by `make patch` (and `make build`):

| Change | Why |
|---|---|
| `0001-wasi-malloc-usable-size.patch` | `js__malloc_usable_size` returned 0 under WASI, so QuickJS counted only 8 bytes per allocation and the soft `MemoryLimit` missed everything but single oversized allocations (300 x 64 KiB strings fit under 8 MiB). wasi-libc's dlmalloc has `malloc_usable_size`. `TestMemoryLimitCountsStrings` |
| `0002-wasi-stack-limit.patch` | `update_stack_limit` hard-coded "no limit" under WASI, so `MaxStackSize` was ignored and deep recursion trapped the VM instead of throwing. The limit now works, never goes below a 64 KiB guard at the bottom of the stack, and cannot wrap when `MaxStackSize` exceeds the linked stack. `TestDeepRecursionThrowsRangeError` |
| `qjswasm.cmake`: link options as `target_link_options` | Upstream called `add_link_options` after `add_executable(qjswasm)`, so `--stack-first` and `--initial-memory` never reached the linker: the wasm had wasm-ld's 64 KiB stack placed after the data, where an overflow overwrites static data before anything traps. Now: `--stack-first`, a 1 MiB stack (QuickJS's default `JS_DEFAULT_STACK_SIZE`), 2 MiB initial memory |

Result: unbounded recursion throws a catchable `RangeError: Maximum call stack size exceeded` at about 3,600 frames by
default (upstream trapped at about 229); `MaxStackSize` below 1 MiB lowers that, above it is clamped. Deeply nested
`JSON.parse`/`JSON.stringify`, `eval` of nested literals, nested regexp groups, getter/Proxy/`toString` recursion and
GC of a 300,000-deep object graph all end with a catchable error or succeed, without trapping.

### Building

Needs [wasi-sdk](https://github.com/WebAssembly/wasi-sdk/releases) (built with wasi-sdk 34, clang 23), CMake and
binaryen's `wasm-opt`:

```sh
git submodule update --init qjswasm/quickjs
make build WASI_SDK_PATH=$HOME/opt/wasi-sdk   # default /opt/wasi-sdk; applies the patches, writes qjs.wasm
git -C qjswasm/quickjs checkout -- .          # keep the submodule clean before committing
```

The built `qjs.wasm` is about 1.34 MB (upstream's was 1.04 MB, built with an older toolchain).

## Upstream test failure on macOS

`TestEvalOptions` ("should fail when the current directory was removed") fails on macOS in upstream too: `os.Getwd`
still succeeds there after the directory is removed. Run the suite with `-skip TestEvalOptions` on macOS.
