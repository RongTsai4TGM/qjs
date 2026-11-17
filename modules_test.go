package qjs_test

import (
	"strings"
	"testing"

	"github.com/RongTsai4TGM/qjs"
)

// importResult runs code as an async global script and returns what it resolved or rejected with.
func importResult(t *testing.T, rt *qjs.Runtime, code string) string {
	t.Helper()
	v, err := rt.Context().Eval("modules.js", qjs.Code(`await (async () => { try { return String(await (async () => { `+code+` })()) } catch (e) { return "threw " + e.name + ": " + e.message } })()`), qjs.FlagAsync())
	if err != nil {
		return "eval error: " + err.Error()
	}
	defer v.Free()
	return v.String()
}

// TestDisableModules checks that with DisableModules no host module (qjs:std, qjs:os, qjs:bjson) is registered and
// every import, however its source was built, fails inside the engine.
func TestDisableModules(t *testing.T) {
	rt, err := qjs.New(qjs.Option{DisableModules: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	for name, code := range map[string]string{
		"literal qjs:os":      `return (await import("qjs:os")).sleep`,
		"literal qjs:std":     `return (await import("qjs:std")).open`,
		"literal qjs:bjson":   `return (await import("qjs:bjson")).read`,
		"relative file":       `return await import("./x.js")`,
		"eval-built":          `const w = "imp" + "ort"; return await eval(w + "('qjs:os')")`,
		"Function-built":      `const w = "imp" + "ort"; return await new Function("return " + w + "('qjs:os')")()`,
		"AsyncFunction-built": `const w = "imp" + "ort"; return await new (Object.getPrototypeOf(async function () {}).constructor)("return " + w + "('qjs:std')")()`,
		"comment before (":    `return await import /* x */ ("qjs:os")`,
	} {
		got := importResult(t, rt, code)
		if !strings.Contains(got, "module loading is disabled") {
			t.Errorf("%s: %s", name, got)
		}
	}
	// No host objects are exposed as globals either.
	if got := importResult(t, rt, `return [typeof std, typeof os, typeof bjson, typeof setTimeout].join()`); got != "undefined,undefined,undefined,undefined" {
		t.Errorf("host globals: %s", got)
	}
	// Source that only mentions import( still runs, and eval is ordinary direct eval.
	if got := importResult(t, rt, `const a = 41; return ["import(x)".length, { import() { return 1 } }.import(), eval("a + 1")].join()`); got != "9,1,42" {
		t.Errorf("non-import code: %s", got)
	}
}

// TestModulesEnabledByDefault keeps upstream's behavior when DisableModules is not set.
func TestModulesEnabledByDefault(t *testing.T) {
	rt, err := qjs.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if got := importResult(t, rt, `return typeof (await import("qjs:std")).open`); got != "function" {
		t.Errorf("import qjs:std without DisableModules: %s", got)
	}
}
