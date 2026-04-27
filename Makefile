TEST_SUITE_DIR := testdata/yaml-test-suite
TEST_SUITE_REPO := https://github.com/yaml/yaml-test-suite.git
TEST_SUITE_TAG := $(shell git ls-remote --tags $(TEST_SUITE_REPO) 'refs/tags/data-*' | grep -v '\^{}' | sed 's|.*refs/tags/||' | sort | tail -1)

.PHONY: test-suite test test-verbose clean-test-suite ci fmt vet staticcheck bench fuzz fuzz-smoke

test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	git clone --branch $(TEST_SUITE_TAG) --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

test: test-suite
	go test ./...

test-verbose: test-suite
	go test -v -run TestYAMLTestSuite ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "files not formatted:" && gofmt -l . && exit 1)

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

bench:
	go test -bench=. -benchmem -count=1 ./... | tee bench.out

fuzz:
	go test -fuzz=FuzzUnmarshal -fuzztime=30s .
	go test -fuzz=FuzzScanner -fuzztime=30s .
	go test -fuzz=FuzzRoundTrip -fuzztime=30s .

ci: test-suite fmt vet staticcheck fuzz
	go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

clean-test-suite:
	rm -rf $(TEST_SUITE_DIR)
