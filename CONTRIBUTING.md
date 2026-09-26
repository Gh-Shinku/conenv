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
