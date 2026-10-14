# Repository settings

The settings of `github.com/velcure/esdscom` are configured through the
GitHub API. This file lists them with the commands that set them, and the
features that are not available while the repository is private.

## Visibility

The repository is private. It becomes public after the eSDScom consortium
has confirmed in writing that the generated Go types may be published
(see the licence section of the README).

## Merging

| Setting | Value |
|---|---|
| Merge methods | squash only |
| Squash commit title / message | pull request title / pull request body |
| Delete head branches after merge | on |
| Auto-merge | off |
| Suggest updating pull request branches | on |
| Wiki, Projects | off |

```sh
gh api -X PATCH repos/velcure/esdscom \
  -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY \
  -F delete_branch_on_merge=true -F allow_auto_merge=false -F allow_update_branch=true \
  -F has_wiki=false -F has_projects=false
```

The pull request title becomes the commit message on `main`, so it follows
Conventional Commits; release-please reads it.

## Security

| Feature | State |
|---|---|
| Secret scanning | on |
| Secret scanning push protection | on |
| Code security (code scanning for CodeQL results) | on |
| Dependabot alerts | on |
| Dependabot security updates | on |
| Private vulnerability reporting | not available while private |

```sh
echo '{"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"},"code_security":{"status":"enabled"}}}' \
  | gh api -X PATCH repos/velcure/esdscom --input -
gh api -X PUT repos/velcure/esdscom/vulnerability-alerts
gh api -X PUT repos/velcure/esdscom/automated-security-fixes
```

Private vulnerability reporting is offered by GitHub for public repositories
only; for this private repository the API answers `404 Not Found`. When the
repository becomes public it is switched on with

```sh
gh api -X PUT repos/velcure/esdscom/private-vulnerability-reporting
```

The repository-level switch `security_and_analysis.advanced_security`
cannot be changed for this repository (`422`: "not available, nor a
pre-requisite for security features"); code scanning works through the
`code_security` setting above.

## Ruleset `main`

A repository ruleset (`main`) applies to the default branch, without bypass
actors:

| Rule | Setting |
|---|---|
| Pull request before merging | 1 approving review, stale reviews dismissed on push, conversations resolved, squash merge only |
| Required status checks | `CI success` (ci.yml), `Analyze (go)` (codeql.yml), `govulncheck` (govulncheck.yml); branch must be up to date |
| Linear history | required |
| Signed commits | required |
| Force pushes | blocked |
| Deletion | blocked |

`CI success` passes only when every job of ci.yml passed: the test matrix
(Linux, macOS, Windows × the two latest Go releases), lint, the generated
code check, the xmllint cross-check, the fuzz run and the coverage job.

```sh
gh api -X POST repos/velcure/esdscom/rulesets --input ruleset.json
```

with `ruleset.json`:

```json
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "bypass_actors": [],
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "required_linear_history" },
    { "type": "required_signatures" },
    { "type": "pull_request", "parameters": {
        "required_approving_review_count": 1,
        "dismiss_stale_reviews_on_push": true,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": true,
        "allowed_merge_methods": ["squash"] } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": true,
        "do_not_enforce_on_create": false,
        "required_status_checks": [
          { "context": "CI success", "integration_id": 15368 },
          { "context": "Analyze (go)", "integration_id": 15368 },
          { "context": "govulncheck", "integration_id": 15368 } ] } }
  ]
}
```

`integration_id` 15368 is the GitHub Actions app.

## Workflows that depend on visibility

| Workflow | While private | When public |
|---|---|---|
| OpenSSF Scorecard | skipped: the workflow token cannot read a private repository through the GraphQL API | runs on push, weekly and on branch protection changes, publishes results and uploads SARIF |
| release-please, job `announce to proxy.golang.org` | skipped: the Go module proxy cannot fetch private modules | requests the new version from proxy.golang.org so that pkg.go.dev lists it |

The README badges for Scorecard and pkg.go.dev show data once the
repository is public.
