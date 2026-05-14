# Development Guide
*(TBD)*

## Running Tests

### Unit Tests

Coverage and race detection are always enabled.

```bash
make test-unit          # run all unit tests
```

## Submitting Changes

Before opening a PR, run:

```bash
make presubmit
```

This runs the same lint, vet, and test checks as the CI pipeline. Fixing failures locally

