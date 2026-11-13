# Pre-commit Configuration

This project uses [pre-commit](https://pre-commit.com/) to prevent accidental commits of credentials, secrets, and other sensitive information.

## Setup

1. **Install pre-commit**:
   ```bash
   pip install pre-commit
   ```

2. **Install the hooks**:
   ```bash
   make pre-commit-install
   ```
   Or manually:
   ```bash
   pre-commit install
   ```

3. **Run hooks on all files**:
   ```bash
   make pre-commit-run
   ```
   Or manually:
   ```bash
   pre-commit run --all-files
   ```

## Configured Hooks

### Secret Detection

The following hooks are configured to detect credentials and secrets:

1. **[detect-secrets](https://github.com/Yelp/detect-secrets)**
   - Scans for secrets in code using multiple detection methods
   - Baseline file: `.secrets.baseline` (auto-generated, excluded from git)
   - Excludes: `go.mod`, `go.sum`, binary files, test data, vendor, generated files

2. **[gitleaks](https://github.com/gitleaks/gitleaks)**
   - Fast secrets detector with customizable rules
   - Scans for passwords, API keys, tokens, credentials, private keys, etc.
   - Excludes: same as detect-secrets

3. **Custom grep-based check**
   - Greps for common credential patterns (password, secret, api_key, token, etc.)
   - Runs on Go, YAML, JSON, TOML, env, and other text files

### Go Code Quality

1. **[golangci-lint](https://github.com/golangci/golangci-lint)**
   - Comprehensive Go linter aggregator
   - Runs on all Go files

2. **[go-vet](https://golang.org/cmd/vet/)**
   - Checks for potential issues in Go code
   - Runs on all Go files

### General Quality

1. **Trailing whitespace**: Removes trailing whitespace from files
2. **Mixed line endings**: Ensures consistent line endings
3. **UTF-8 BOM**: Removes UTF-8 BOM from files
4. **Large files**: Prevents committing files >500KB

## Baseline File

The `.secrets.baseline` file is used by `detect-secrets` to avoid flagging known secrets (like test data). It is auto-generated and **excluded from git** via `.gitignore`.

## Updating Hooks

To update all hooks to their latest versions:
```bash
pre-commit autoupdate
```

## Bypassing Hooks

To bypass pre-commit hooks for a specific commit (not recommended):
```bash
SKIP=detect-secrets,gitleaks git commit -m "message"
```

Or to skip all hooks:
```bash
git commit --no-verify -m "message"
```

## Customization

Edit `.pre-commit-config.yaml` to:
- Add or remove hooks
- Change hook versions
- Modify exclusion patterns
- Adjust hook arguments

## Requirements

- Python 3.7+
- pip
- pre-commit package

All dependencies for the hooks will be automatically installed when you run `pre-commit install` or `pre-commit run --all-files`.
