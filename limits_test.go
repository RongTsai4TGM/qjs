package qjs_test

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RongTsai4TGM/qjs"
)

// heapInUse returns the live heap after a full collection.
func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// TestCloseReleasesEverything checks that New + Close does not leak host memory: Close must close the per-runtime wazero
// runtime (with its WASI and env host modules), not only the guest module.
func TestCloseReleasesEverything(t *testing.T) {
	newClose := func() {
		rt, err := qjs.New()
		if err != nil {
			t.Fatal(err)
		}
		rt.Close()
	}
	for range 20 { // warm up the global compiled module and caches
		newClose()
	}
	before := heapInUse()
	const n = 500
	for range n {
		newClose()
	}
	growth := int64(heapInUse()) - int64(before)
	t.Logf("heap growth over %d New+Close: %d bytes (%d per runtime)", n, growth, growth/n)
	if growth > 1<<20 {
		t.Errorf("heap grew %d bytes over %d New+Close; Close leaks", growth, n)
	}
}

// TestMemoryLimitCountsStrings checks whether JS_SetMemoryLimit accounts for large string allocations in the wasm build.
// Upstream's build reported 0 from malloc_usable_size under WASI, so only 8 bytes per allocation were counted
// (qjswasm/patches/0001-wasi-malloc-usable-size.patch).
func TestMemoryLimitCountsStrings(t *testing.T) {
	rt, err := qjs.New(qjs.Option{MemoryLimit: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	// 300 distinct 64 KiB strings = ~19 MiB, well past the 8 MiB limit.
	v, err := rt.Context().Eval("strings.js", qjs.Code(`
		const keep = [];
		let err = "";
		try {
			for (let i = 0; i < 300; i++) keep.push(String(i).padEnd(65536, "x"));
		} catch (e) { err = String(e); }
		JSON.stringify({ n: keep.length, err });
	`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	defer v.Free()
	got := v.String()
	t.Logf("result: %s; wasm memory now %d MiB", got, rt.Mem().Size()>>20)
	if !strings.Contains(got, "out of memory") {
		t.Errorf("300 x 64 KiB strings fit under an 8 MiB MemoryLimit: %s", got)
	}
}

// TestMaxMemoryPagesBoundsStrings checks the hard cap: with MaxMemoryPages the same string-heavy script fails with a
// catchable "out of memory" instead of growing host memory, and the runtime stays usable.
func TestMaxMemoryPagesBoundsStrings(t *testing.T) {
	const pages = 256 // 16 MiB
	rt, err := qjs.New(qjs.Option{MaxMemoryPages: pages})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	v, err := rt.Context().Eval("strings.js", qjs.Code(`
		const keep = [];
		let err = "";
		try {
			for (let i = 0; i < 2000; i++) keep.push(String(i).padEnd(65536, "x"));
		} catch (e) { err = String(e); }
		keep.length = 0;
		JSON.stringify({ err });
	`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got := v.String()
	v.Free()
	t.Logf("result: %s; wasm memory %d MiB", got, rt.Mem().Size()>>20)
	if !strings.Contains(got, "out of memory") {
		t.Errorf("2000 x 64 KiB strings did not hit the %d-page cap: %s", pages, got)
	}
	if size := rt.Mem().Size(); size > pages*65536 {
		t.Errorf("wasm memory is %d bytes, over the %d-page cap", size, pages)
	}
	v2, err := rt.Context().Eval("after.js", qjs.Code(`1 + 1`))
	if err != nil {
		t.Fatalf("runtime unusable after out of memory: %v", err)
	}
	defer v2.Free()
	if v2.Int32() != 2 {
		t.Errorf("1 + 1 = %d after out of memory", v2.Int32())
	}
}

// TestCloseAfterContextDoneReleasesEverything checks the abort path: when the context ends, wazero closes the guest
// module (CloseOnContextDone), so freeing the QuickJS runtime in Close panics. Close must still close the wazero
// runtime, or every aborted or timed-out runtime leaks.
func TestCloseAfterContextDoneReleasesEverything(t *testing.T) {
	abortAndClose := func() {
		ctx, cancel := context.WithCancel(context.Background())
		rt, err := qjs.New(qjs.Option{Context: ctx, CloseOnContextDone: true})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { _ = recover() }()
			if v, err := rt.Context().Eval("loop.js", qjs.Code(`for (;;) {}`)); err == nil {
				v.Free()
			}
		}()
		time.Sleep(time.Millisecond)
		cancel()
		<-done
		func() {
			defer func() { _ = recover() }() // freeing the QuickJS runtime of a closed module may panic
			rt.Close()
		}()
	}
	for range 10 {
		abortAndClose()
	}
	before := heapInUse()
	const n = 200
	for range n {
		abortAndClose()
	}
	growth := int64(heapInUse()) - int64(before)
	t.Logf("heap growth over %d aborted runtimes: %d bytes (%d per runtime)", n, growth, growth/n)
	if growth > 1<<20 {
		t.Errorf("heap grew %d bytes over %d aborted runtimes; Close leaks on the abort path", growth, n)
	}
}

// TestDeepRecursionThrowsRangeError checks the stack limit in the wasm build. Upstream disabled QuickJS's stack check
// under WASI and its link options never reached the linker (64 KiB stack after the data), so recursion trapped the VM
// at about 229 frames (qjswasm/patches/0002-wasi-stack-limit.patch, target_link_options in qjswasm.cmake).
func TestDeepRecursionThrowsRangeError(t *testing.T) {
	// maxDepth runs unbounded recursion and returns how deep it got and what it threw; the runtime must stay usable.
	maxDepth := func(t *testing.T, maxStack int) (int, string) {
		t.Helper()
		rt, err := qjs.New(qjs.Option{MaxStackSize: maxStack})
		if err != nil {
			t.Fatal(err)
		}
		defer rt.Close()
		v, err := rt.Context().Eval("recursion.js", qjs.Code(`
			let n = 0, err = "";
			function f() { n++; f(); }
			try { f(); } catch (e) { err = e.name + ": " + e.message; }
			JSON.stringify({ n, err });
		`))
		if err != nil {
			t.Fatalf("eval: %v", err)
		}
		var got struct {
			N   int    `json:"n"`
			Err string `json:"err"`
		}
		if err := json.Unmarshal([]byte(v.String()), &got); err != nil {
			t.Fatal(err)
		}
		v.Free()
		v2, err := rt.Context().Eval("after.js", qjs.Code(`1 + 1`))
		if err != nil {
			t.Fatalf("runtime unusable after stack overflow: %v", err)
		}
		defer v2.Free()
		if v2.Int32() != 2 {
			t.Errorf("1 + 1 = %d after stack overflow", v2.Int32())
		}
		return got.N, got.Err
	}

	def, defErr := maxDepth(t, 0)           // JS_DEFAULT_STACK_SIZE (1 MiB), clamped to the linked stack
	small, smallErr := maxDepth(t, 256<<10) // below the linked stack: honored
	large, largeErr := maxDepth(t, 4<<20)   // above the linked stack: clamped, must not wrap
	t.Logf("max depth: default %d, 256 KiB %d, 4 MiB %d", def, small, large)
	for name, e := range map[string]string{"default": defErr, "256 KiB": smallErr, "4 MiB": largeErr} {
		if !strings.Contains(e, "RangeError") {
			t.Errorf("%s: unbounded recursion did not throw a RangeError: %q", name, e)
		}
	}
	if def < 2000 {
		t.Errorf("default max depth %d; want well past upstream's ~229", def)
	}
	if small >= def/2 {
		t.Errorf("MaxStackSize 256 KiB reached depth %d, default %d: the limit is not honored", small, def)
	}
	if large < def*9/10 || large > def*11/10 {
		t.Errorf("MaxStackSize 4 MiB reached depth %d, default %d: want about the same (clamped to the linked stack)", large, def)
	}
}
