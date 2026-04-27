TEST_SUITE_DIR := testdata/yaml-test-suite
TEST_SUITE_REPO := https://github.com/yaml/yaml-test-suite.git
TEST_SUITE_TAG := $(shell git ls-remote --tags $(TEST_SUITE_REPO) 'refs/tags/data-*' | grep -v '\^{}' | sed 's|.*refs/tags/||' | sort | tail -1)

.PHONY: all clean clone-test-suite go test test-bench test-conformance test-fuzz test-race

all: test test-bench test-conformance test-fuzz test-race
	@go tool cover -func=coverage.out | tail -1

clean:
	rm -rf $(TEST_SUITE_DIR)

clone-test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	@git clone --branch $(TEST_SUITE_TAG) --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

go:
	go fmt ./...
	go vet ./...
	staticcheck ./...
	go-licenses check ./...
	govulncheck ./...

test: clone-test-suite
	@go test -v -cover . | tee test.out
	@go tool cover -func=test.out | tail -1

test-bench:
	@go test -bench=. -benchmem -count=1 . | tee test_bench.out
	@go tool cover -func=test_bench.out | tail -1

test-conformance: clone-test-suite
	@go test -v -run -cover TestYAMLTestSuite ./... | tee test_conformance.out
	@go tool cover -func=test_conformance.out | tail -1

test-fuzz:
	@go test -fuzz=FuzzUnmarshal -fuzztime=60s . | tee test_fuzz_unmarshal.out
	@go tool cover -func=test_fuzz_unmarshal.out | tail -1
	@go test -fuzz=FuzzScanner -fuzztime=60s . | tee test_fuzz_scanner.out
	@go tool cover -func=test_fuzz_scanner.out | tail -1
	@go test -fuzz=FuzzRoundTrip -fuzztime=60s . | tee test_fuzz_roundtrip.out
	@go tool cover -func=test_fuzz_roundtrip.out | tail -1

test-race:
	@go test -race -coverprofile=test_conformance.out . | tee test_race.out
	@go tool cover -func=test_race.out | tail -1
