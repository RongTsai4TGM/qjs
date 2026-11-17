.PHONY: build build-debug clean patch

# WASI_SDK_PATH points at an unpacked wasi-sdk. wasi-sdk 25+ ships wasi-sdk-p1.cmake (WASI preview 1, what wazero
# runs); older releases ship wasi-sdk.cmake.
WASI_SDK_PATH ?= /opt/wasi-sdk
WASI_TOOLCHAIN := $(firstword $(wildcard $(WASI_SDK_PATH)/share/cmake/wasi-sdk-p1.cmake $(WASI_SDK_PATH)/share/cmake/wasi-sdk.cmake))
NPROC := $(shell nproc 2>/dev/null || sysctl -n hw.ncpu)

# patch resets the quickjs-ng submodule and applies this fork's patches (see FORK.md).
patch:
		git -C qjswasm/quickjs checkout -- .
		for p in $(CURDIR)/qjswasm/patches/*.patch; do git -C qjswasm/quickjs apply "$$p" || exit 1; done

build: patch
		@echo "Configuring and building qjs..."
		cd qjswasm/quickjs && \
		rm -rf build && \
		cmake -B build \
				-DQJS_BUILD_LIBC=ON \
				-DQJS_BUILD_CLI_WITH_MIMALLOC=OFF \
				-DCMAKE_TOOLCHAIN_FILE=$(WASI_TOOLCHAIN) \
				-DWASI_SDK_PREFIX=$(WASI_SDK_PATH) \
				-DCMAKE_PROJECT_INCLUDE=../qjswasm.cmake
		@echo "Building qjs target..."
		make -C qjswasm/quickjs/build qjswasm -j$(NPROC)
		@echo "Copying build/qjswasm to top-level as qjs.wasm..."
		cp qjswasm/quickjs/build/qjswasm qjs.wasm

		wasm-opt -O3 qjs.wasm -o qjs.wasm

build-debug: patch
		@echo "Configuring and building qjs with runtime address debug..."
		cd qjswasm/quickjs && \
		rm -rf build && \
		cmake -B build \
				-DQJS_BUILD_LIBC=ON \
				-DQJS_BUILD_CLI_WITH_MIMALLOC=OFF \
				-DQJS_DEBUG_RUNTIME_ADDRESS=ON \
				-DCMAKE_TOOLCHAIN_FILE=$(WASI_TOOLCHAIN) \
				-DWASI_SDK_PREFIX=$(WASI_SDK_PATH) \
				-DCMAKE_PROJECT_INCLUDE=../qjswasm.cmake
		@echo "Building qjs target..."
		make -C qjswasm/quickjs/build qjswasm -j$(NPROC)
		@echo "Copying build/qjswasm to top-level as qjs.wasm..."
		cp qjswasm/quickjs/build/qjswasm qjs.wasm

		wasm-opt -O3 qjs.wasm -o qjs.wasm

clean:
	@echo "Cleaning build directory..."
	cd quickjs && rm -rf build

test:
	./test.sh

lint:
	golangci-lint run
