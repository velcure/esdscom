# Repository settings

The settings of `github.com/velcure/esdscom` are configured through the
GitHub API. This file lists them with the commands that set them.

## Visibility

The repository is public.

```sh
gh api -X PATCH repos/velcure/esdscom -F private=false
```

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
| Secret scanning validity checks | on |
| Secret scanning non-provider patterns | on |
| Code security (code scanning for CodeQL results) | on |
| Dependabot alerts | on |
| Dependabot security updates | on |
| Private vulnerability reporting | on |

```sh
echo '{"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"},"secret_scanning_validity_checks":{"status":"enabled"},"secret_scanning_non_provider_patterns":{"status":"enabled"},"code_security":{"status":"enabled"}}}' \
  | gh api -X PATCH repos/velcure/esdscom --input -
gh api -X PUT repos/velcure/esdscom/vulnerability-alerts
gh api -X PUT repos/velcure/esdscom/automated-security-fixes
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
| Pull request before merging | no required approving review while the repository has one maintainer, stale reviews dismissed on push, conversations resolved, squash merge only |
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
        "required_approving_review_count": 0,
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

| Workflow | Behaviour |
|---|---|
| OpenSSF Scorecard | runs on push, weekly and on branch protection changes, publishes results and uploads SARIF; skipped on a private repository |
| release-please, job `announce to proxy.golang.org` | requests the new version from proxy.golang.org so that pkg.go.dev lists it; skipped on a private repository |
