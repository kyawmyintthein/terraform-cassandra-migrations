# Contributing

Thanks for your interest in improving this Terraform provider.

## Before you start

- Read the [README](README.md) for the provider model and ownership boundaries.
- Read [AGENTS.md](AGENTS.md) for repository-specific implementation and AI-assisted coding guidance.
- Check existing issues and pull requests before starting work.
- Open an issue first for large changes so the approach can be discussed early.

## Development workflow

1. Fork the repository and create a branch for your work.
2. Make focused changes with clear commit messages.
3. Run the local checks before opening a pull request.
4. Update docs and examples when behavior changes.

## Local checks

```bash
gofmt -w .
go vet ./...
go test ./...
./scripts/e2e-test.sh
```

If you use [`just`](https://github.com/casey/just), the repository also includes shortcuts for the common commands:

```bash
just fmt
just vet
just test
just e2e
```

## Pull request expectations

- Explain the problem being solved.
- Call out any schema migration risks or compatibility considerations.
- Include example updates when user-facing behavior changes.
- Keep PRs small enough for review when possible.

## Design guidance

- Preserve the separation between user-level schema ownership and system-level operational ownership.
- Avoid introducing destructive defaults.
- Prefer explicit validation and clear error messages for unsafe changes.

## Reporting security issues

Please do not report suspected security issues in public issues. Follow the guidance in [SECURITY.md](SECURITY.md).
