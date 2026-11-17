# Fork notes

Fork of [fastschema/qjs](https://github.com/fastschema/qjs) v0.0.6 (MIT), used by agentkit's `ext/codemode`.

The module path is `github.com/RongTsai4TGM/qjs`, not the upstream one. A `replace` directive in a library's go.mod is
ignored by the programs that import that library, so a renamed module is the only way agentkit's users get these fixes.

## Changes from upstream

| Change | Why |
|---|---|
| `Runtime.Close` also closes the per-runtime wazero runtime | `New` creates one per `Runtime` (with the WASI and `env` host modules) and upstream never closed it: about 23 KB of host memory leaked per runtime. `TestCloseReleasesEverything` |
| `Option.MaxMemoryPages` | Hard cap on the wasm linear memory (wazero `WithMemoryLimitPages`). Upstream's `MemoryLimit` relies on QuickJS's own accounting, which misses large allocations in the wasm build, so a script could grow host memory up to the 4 GiB wasm32 limit. With the cap, the script gets a catchable `InternalError: out of memory` and the runtime stays usable. `TestMaxMemoryPagesBoundsStrings` |

## Known, not yet fixed (need a `qjs.wasm` rebuild)

- `MemoryLimit` (soft limit) does not count large allocations. `TestMemoryLimitCountsStrings` is skipped until then.
- `MaxStackSize` is ignored. The wasm is linked with wasm-ld's default 64 KiB stack (`--stack-first`), so recursion
  traps at roughly 229 frames ("out of bounds memory access") instead of throwing a catchable RangeError.

## Upstream test failure on macOS

`TestEvalOptions` ("should fail when the current directory was removed") fails on macOS in upstream too: `os.Getwd`
still succeeds there after the directory is removed. Run the suite with `-skip TestEvalOptions` on macOS.
