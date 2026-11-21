# Contributing

## Commit messages

Every commit message must contain exactly one line and use this format:

```text
[<type>[/<detail>]] <description>
```

Rules:

- `type` is required and uses lowercase letters, digits, or hyphens; it must start with a letter.
- `detail` is optional and follows the same naming rules as `type`.
- Exactly one space must follow the closing `]`.
- `description` must be non-empty and remain on a single line.
- Commit bodies and other additional lines are not allowed.

Valid examples:

```text
[feat] add profile inheritance
[fix/windows] preserve existing symlinks
[docs/configuration] document secret references
```

## Build and release

The [Build and release workflow](.github/workflows/release.yml) runs `go vet ./...` and `go test ./...` on Windows, Linux, and macOS. It builds `./cmd/conenv` for `amd64` and `arm64` on all three operating systems. The `conenv-packages` artifact contains the six archives and `SHA256SUMS`. Pushes to `main`, pull requests targeting `main`, and manual runs build packages for testing.

To publish a version, push a `vMAJOR.MINOR.PATCH` tag for a commit on `main`. For example, from an up-to-date `main`:

```sh
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
```

After the tests and builds pass, GitHub Actions publishes the archives and checksums in a GitHub Release with generated release notes.
