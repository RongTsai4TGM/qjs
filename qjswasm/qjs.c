#include "qjs.h"

/* Set by New_QJS from QJS_FLAG_DISABLE_MODULES. Every Go Runtime is its own wasm
   instance, so this global belongs to one runtime. */
static bool qjs_modules_disabled = false;

/* Module loader used with QJS_FLAG_DISABLE_MODULES. import() of any name, static
   or dynamic and however its source was built, ends here and rejects. */
static JSModuleDef *QJS_DisabledModuleLoader(JSContext *ctx, const char *module_name, void *opaque)
{
  JS_ThrowTypeError(ctx, "cannot import '%s': module loading is disabled", module_name);
  return NULL;
}

JSContext *New_QJSContext(JSRuntime *rt)
{
  JSContext *ctx;
  ctx = JS_NewContext(rt);
  if (!ctx)
    return NULL;
  /* A registered module resolves by name before the loader is asked, so with
     modules disabled the host modules must not be registered at all. */
  if (!qjs_modules_disabled)
  {
    js_init_module_std(ctx, "qjs:std");
    js_init_module_os(ctx, "qjs:os");
    js_init_module_bjson(ctx, "qjs:bjson");
  }
  js_set_global_objs(ctx, !qjs_modules_disabled);

  return ctx;
}

QJSRuntime *New_QJS(
    size_t memory_limit,
    size_t max_stack_size,
    size_t max_execution_time,
    size_t gc_threshold,
    size_t flags)
{
  JSRuntime *runtime;
  JSContext *ctx;

#ifdef QJS_DEBUG_RUNTIME_ADDRESS
  randomize_address_space();
#endif

  runtime = JS_NewRuntime();

  if (!runtime)
    return NULL;

  if (memory_limit > 0)
    JS_SetMemoryLimit(runtime, memory_limit);

  if (gc_threshold > 0)
    JS_SetGCThreshold(runtime, gc_threshold);

  if (max_stack_size > 0)
    JS_SetMaxStackSize(runtime, max_stack_size);

  /* setup the the worker context */
  js_std_set_worker_new_context_func(New_QJSContext);
  /* initialize the standard objects */
  js_std_init_handlers(runtime);
  /* loader for ES6 modules */
  qjs_modules_disabled = (flags & QJS_FLAG_DISABLE_MODULES) != 0;
  JS_SetModuleLoaderFunc(runtime, NULL,
                         qjs_modules_disabled ? QJS_DisabledModuleLoader : QJS_ModuleLoader,
                         NULL);
  /* exit on unhandled promise rejections */
  // JS_SetHostPromiseRejectionTracker(runtime, js_std_promise_rejection_tracker, NULL);

  ctx = New_QJSContext(runtime);
  if (!ctx)
  {
    JS_FreeRuntime(runtime);
    return NULL;
  }

  // Initialize QJS_PROXY_VALUE class
  if (init_qjs_proxy_value_class(ctx) < 0)
  {
    JS_FreeContext(ctx);
    JS_FreeRuntime(runtime);
    return NULL;
  }

  QJSRuntime *qjs = (QJSRuntime *)malloc(sizeof(QJSRuntime));
  if (!qjs)
  {
    JS_FreeContext(ctx);
    JS_FreeRuntime(runtime);
    return NULL;
  }

  qjs->runtime = runtime;
  qjs->context = ctx;

  return qjs;
}

void QJS_FreeValue(JSContext *ctx, JSValue val)
{
  JS_FreeValue(ctx, val);
}

void QJS_Free(QJSRuntime *qjs)
{
  JS_FreeContext(qjs->context);
  JS_FreeRuntime(qjs->runtime);
  free(qjs);
}

JSValue QJS_CloneValue(JSContext *ctx, JSValue val)
{
  return JS_DupValue(ctx, val);
}

JSContext *QJS_GetContext(QJSRuntime *qjs)
{
  return qjs->context;
}

void QJS_UpdateStackTop(QJSRuntime *qjs)
{
  JS_UpdateStackTop(qjs->runtime);
}

QJSRuntime *qjs = NULL;

QJSRuntime *QJS_GetRuntime()
{
  return qjs;
}

void initialize()
{
  if (qjs != NULL)
    return;
  size_t memory_limit = 0;
  size_t gc_threshold = 0;
  size_t max_stack_size = 0;
  qjs = New_QJS(memory_limit, max_stack_size, 0, gc_threshold, 0);
}
