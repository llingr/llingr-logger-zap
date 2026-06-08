UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Race detector works on macOS (any arch) and Linux x86_64
# Disabled on Linux ARM64 due to ThreadSanitizer VMA limitation
ifeq ($(UNAME_S)-$(filter aarch64 arm%,$(UNAME_M)),Linux-$(UNAME_M))
  RACE :=
else
  RACE := -race
endif

default: build test

build:
	go build ./...

test:
	go test $(RACE) -coverprofile=coverage.out -coverpkg=./zaplogger/... ./...
	go tool cover -func=coverage.out
