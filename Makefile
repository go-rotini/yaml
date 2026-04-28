TEST_SUITE_DIR := testdata/yaml-test-suite
TEST_SUITE_REPO := https://github.com/yaml/yaml-test-suite.git
TEST_SUITE_TAG := $(shell git ls-remote --tags $(TEST_SUITE_REPO) 'refs/tags/data-*' | grep -v '\^{}' | sed 's|.*refs/tags/||' | sort | tail -1)

.PHONY: all clean clone-test-suite test test-acceptance test-bench test-conformance test-fuzz test-mutation test-race yaml

all: yaml test test-acceptance test-bench test-conformance test-fuzz test-mutation test-race

clean:
	@rm -rf $(TEST_SUITE_DIR) *.out

clone-test-suite: $(TEST_SUITE_DIR)

$(TEST_SUITE_DIR):
	@git clone --quiet --branch $(TEST_SUITE_TAG) --depth 1 $(TEST_SUITE_REPO) $(TEST_SUITE_DIR)

test: clone-test-suite
	@go test -v -count=1 -coverprofile=test.out .
	@go tool cover -func=test.out | tail -1

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out .
	@go tool cover -func=test_acceptance.out | tail -1

test-bench:
	@go test -bench=. -benchmem -count=1 . | tee test_bench.out

test-conformance: clone-test-suite
	@go test -v -count=1 -run TestYAMLTestSuite -coverprofile=test_conformance.out .
	@go tool cover -func=test_conformance.out | tail -1

test-fuzz:
	@go test -fuzz=FuzzUnmarshal -fuzztime=60s .
	@go test -fuzz=FuzzScanner -fuzztime=60s .
	@go test -fuzz=FuzzRoundTrip -fuzztime=60s .

test-mutation: clone-test-suite
	@go tool github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out .
	@go tool cover -func=test_race.out | tail -1

yaml: clean clone-test-suite
	@go mod download
	@test -z "$$(gofmt -l .)" || (echo "files not formatted:" && gofmt -l . && exit 1)
	go vet ./...
	go tool golangci-lint run ./...
	go tool go-licenses check ./...
	go tool govulncheck ./...
