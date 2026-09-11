# Security policy

## Reporting a vulnerability

Report suspected vulnerabilities privately through
[GitHub private vulnerability reporting](https://github.com/caelis-labs/acp-go-sdk/security/advisories/new).
Do not disclose an unpatched vulnerability in a public issue or pull request.

Include the affected version or commit, operating system, transport, relevant
connection limits, impact, and minimal reproduction steps. Use synthetic data
and redact credentials, private prompts, file contents, and environment values.

Reports may concern message validation, resource exhaustion, request isolation,
cancellation, transport handling, or owned subprocess cleanup. Applications
remain responsible for authentication, authorization, sandbox policy, trusted
handlers, and safe handling of model or tool output; these are not SDK policies.
Ordinary bugs and feature requests belong in public issues.

## Versions and disclosure

Please check whether the issue also affects the latest stable release. Older
versions have no guaranteed security backports; report the exact version tested
even if upgrading is not possible. Experimental packages follow draft protocols
and may change incompatibly; security reports about them are also welcome.

Maintainers coordinate investigation, remediation, and disclosure in the private
advisory. Timing depends on impact and maintainer availability; no fixed response
or remediation deadline is promised.

Only test systems and data you are authorized to assess. Avoid accessing other
users' data or disrupting services while reproducing a vulnerability.
