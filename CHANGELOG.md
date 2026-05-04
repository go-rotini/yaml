# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- YAML 1.2.2 scanner, parser, encoder, and decoder
- Marshal/Unmarshal with struct tag support
- Multi-document streaming via Encoder/Decoder
- YAMLPath query engine
- YAMLToJSON and JSONToYAML helpers
- UTF-8, UTF-16, and UTF-32 encoding detection
- Anchor/alias support with cycle detection
- Merge key support
- Fuzz testing corpus
- YAML test suite conformance tests
- DoS protection defaults; max nesting depth 100, max alias expansion 1000
