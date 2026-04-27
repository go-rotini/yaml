# Contributing

Contributions are welcome! Here's how to get started.

## Setup

```bash
git clone https://github.com/go-rotini/yaml.git
cd yaml
make all   # run all project processes
```

## Making Changes

1. Fork the repository and create a branch from `main`.
2. Write tests for any new functionality.
3. Ensure `make all` passes before submitting a pull request.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (e.g., `feat:`, `fix:`, `test:`, `docs:`).

## Running Tests

```bash
make test           # run all tests
make test-verbose   # run YAML test suite with verbose output
make bench          # run benchmarks
make fuzz           # run fuzz tests (30s per fuzzer)
```

## Pull Requests

- Keep PRs focused on a single change.
- Include tests that cover the change.
- Reference any relevant issues.

## Reporting Bugs

Open an issue with a minimal reproducing YAML input and the expected vs. actual behavior.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
