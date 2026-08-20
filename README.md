# git-email

English | [日本語](README-ja.md)

`git-email` is a Go CLI that scans the Git history of repositories owned by a GitHub user and reports repositories containing email addresses. Scans are read-only by default. An explicitly confirmed remediation option can replace matching commit author/committer addresses and force-push the rewritten history.

- Lists repositories owned by the authenticated user, including public and private repositories
- Scans commit author and committer metadata reachable from every branch and tag
- Scans all current and historical Git blobs
- Supports exact matching for specified addresses and automatic email detection
- Produces human-readable text or machine-readable JSON
- Can optionally replace matching commit metadata with the authenticated user's GitHub noreply address

Repositories are temporarily cloned as mirrors for scanning. These local temporary copies are cleaned up when the command finishes.

## Requirements

- Go 1.25 or later
- Git
- A GitHub Personal Access Token when scanning private repositories or rewriting history

## Build

```bash
go build -o git-email ./cmd/git-email
```

## Configure a token

To scan every public and private repository without omissions, create a fine-grained GitHub personal access token with the following settings:

- Resource owner: the account being scanned
- Repository access: All repositories
- Repository permissions:
  - Metadata: Read (granted automatically)
  - Contents: Read-only

Commit rewriting additionally requires `Contents: Read and write`. Repository rules and branch protection must allow the authenticated user to force-push the affected refs.

Pass the token through the `GITHUB_TOKEN` environment variable. Do not include it in command arguments or clone URLs.

```bash
read -s GITHUB_TOKEN
export GITHUB_TOKEN
```

The tool verifies that the authenticated username matches the requested owner. It compares the repository counts returned by the API with the account totals when GitHub supplies those totals. Fine-grained tokens may omit the private repository total; in that case the tool validates the public total and requires every repository returned by `GET /user/repos` to scan successfully. If an available count differs, the results are displayed, but the scan is marked as incomplete.

## Usage

### Search for a specific email address

`--email` can be specified more than once. Matching is case-insensitive.

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  username
```

A GitHub profile URL can also be used:

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  https://github.com/username
```

### Detect email addresses automatically

When `--email` is omitted, the tool reports every email-like string it finds. GitHub noreply addresses, including addresses under `users.noreply.github.com`, are excluded automatically.

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan username
```

Automatic detection also reports addresses intentionally published in files such as a README. Use `--email` when you only want to check a specific personal address.

### Scan public repositories only

```bash
./git-email scan --public-only username
```

### Produce JSON output

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --format json \
  --email user@example.com \
  username
```

### Change concurrency

By default, up to four repositories are scanned concurrently.

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan --jobs 2 username
```

### Rewrite matching commit emails

> [!CAUTION]
> This operation rewrites Git history and force-pushes changed branches and tags. Commit SHAs change, commit/tag signatures in the rewritten history become invalid, and collaborators must rebase or clone again. Forks, cached commit pages, and external copies may continue to contain the original address.

Rewriting is disabled unless all of the following are supplied:

- One or more explicit `--email` values; automatic detection cannot be used for rewriting
- `--rewrite-commits`
- `--yes` as destructive-operation confirmation
- A token belonging to the requested owner with write access to every affected repository

```bash
GITHUB_TOKEN="$GITHUB_TOKEN" ./git-email scan \
  --email user@example.com \
  --rewrite-commits \
  --yes \
  username
```

The tool preserves file trees, commit messages, names, and timestamps. Matching author/committer addresses are replaced with `ID+USERNAME@users.noreply.github.com`. Only branches and tags whose hashes changed are pushed, using an atomic `--force-with-lease` update to avoid overwriting concurrent remote changes. Addresses found in file contents are reported but are not modified.

## Understanding the output

```text
EXPOSED https://github.com/example/public-repo
  email: person@example.com
  visibility: public
  sources: blob, commit_author
  matches: 3
```

- `EXPOSED`: the address was found in a public repository and is externally accessible
- `PRIVATE_FINDING`: the address was found in a private repository; this does not mean it has been publicly exposed
- `commit_author`: email from commit author metadata
- `commit_committer`: email from commit committer metadata
- `blob`: email found in Git-managed file content
- `REWRITTEN`: matching commit metadata was replaced and the listed refs were force-pushed successfully

Matches are grouped by repository and email address. Each finding includes the total match count and up to five representative SHA/path records. File contents are never printed.

## Exit codes

| Code | Meaning |
| ---: | --- |
| `0` | The scan completed without finding an email address |
| `1` | One or more email addresses were found |
| `2` | The scan was incomplete because of an authentication, permission, API, clone, or similar error |

If findings and errors are both present, the tool displays the findings and exits with code `2`.

A successful rewrite still exits with code `1` because the report records addresses found during the original scan. A rewrite or push failure exits with code `2`.

## Out of scope

- Git objects unreachable from every branch and tag
- Git LFS payloads (the Git-tracked LFS pointer is scanned)
- Decompressed contents of archives or encrypted files
- Issues, pull request comments, wikis, releases, and Actions logs
- Content from external repositories referenced by submodules

## Security

- The GitHub API is only accessed with GET requests.
- Normal scans only invoke read-only Git operations and never push or rewrite history.
- Force-push is only enabled by the combined `--rewrite-commits --yes --email ...` options.
- Tokens are never placed in Git arguments, clone URLs, reports, or error messages.
- Temporary mirrors, including mirrors of private repositories, are cleaned up after successful, failed, or cancelled scans.

## Development checks

```bash
go test -race ./...
go vet ./...
go build ./cmd/git-email
```
