TEST_SUITE_DIR := testdata/yaml-test-suite
TEST_SUITE_REPO := https://github.com/yaml/yaml-test-suite.git
TEST_SUITE_TAG := $(shell git ls-remote --tags $(TEST_SUITE_REPO) 'refs/tags/data-*' | grep -v '\^{}' | sed 's|.*refs/tags/||' | sort | tail -1)

.PHONY: all clean compliance test test-verbose fmt vet staticcheck bench fuzz fuzz-smoke

clean:
	rm -rf $(TEST_SUITE_DIR)

clone-test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	@git clone --branch $(TEST_SUITE_TAG) --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

test: clone-test-suite
	@go test -v -cover . | tee test.out

test-bench:
	@go test -bench=. -benchmem -count=1 . | tee test_bench.out

test-conformance: clone-test-suite
	@go test -v -run -cover TestYAMLTestSuite ./... | tee test_conformance.out

test-fuzz:
	@go test -fuzz=FuzzUnmarshal -fuzztime=60s . | tee test_fuzz_unmarshal.out
	@go test -fuzz=FuzzScanner -fuzztime=60s . | tee test_fuzz_scanner.out
	@go test -fuzz=FuzzRoundTrip -fuzztime=60s . | tee test_fuzz_roundtrip.out

test-race:
	@go test -race -coverprofile=test_conformance.out . | tee test_race.out

go:
	go fmt ./...
	go vet ./...
	staticcheck ./...
	go-licenses check ./...
	govulncheck ./...

all: test test-bench test-conformance test-fuzz test-race
	@go tool cover -func=coverage.out | tail -1
