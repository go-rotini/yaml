# Contributing

Contributions are welcome! Here's how to get started.

## Setup

```bash
git clone https://github.com/go-rotini/yaml.git
cd yaml
go mod download
make all   # run all project processes
```

## Making Changes

1. Fork the repository and create a branch from `main`.
2. Write tests for any new functionality.
3. Ensure `make all` passes before submitting a pull request.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (e.g., `feat:`, `fix:`, `test:`, `docs:`).

## Linting

```bash
make lint
```

## Testing

```bash
make test              # run tests
make test-acceptance   # run YAML acceptance tests verifying encoding/decoding behavior
make test-bench        # run benchmarks
make test-conformance  # run YAML test suite conformance tests
make test-fuzz         # run fuzz tests (60s per fuzzer)
make test-mutation     # run mutation tests
make test-race         # run tests with race detector
```

## Pull Requests

- Keep PRs focused on a single change.
- Include tests that cover the change.
- Reference any relevant issues.

## Reporting Bugs

Open an issue with a minimal reproducing YAML input and the expected vs. actual behavior.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
