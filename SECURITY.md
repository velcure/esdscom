# Security policy

## Supported versions

| Version | Supported |
|---|---|
| latest v0.x release | yes |
| older releases | no |

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's private vulnerability
reporting: on the repository page, open **Security → Report a
vulnerability**. Do not open a public issue.

Include the affected version, a description, and if possible an input
document or code that reproduces the problem.

## Response times

- acknowledgement within 3 working days
- first assessment within 10 working days
- a fix or mitigation for confirmed issues as soon as possible, typically
  within 30 days, followed by a release and a GitHub security advisory

## Scope

esdscom parses untrusted XML. Relevant issues include crashes, excessive
memory or CPU use below the configured size limit, and any way to make it
resolve external entities or access the network or file system.
