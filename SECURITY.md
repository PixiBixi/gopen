# Security Policy

## Supported versions

Only the latest release receives fixes. Older tags are not patched.

## Reporting a vulnerability

Report privately through GitHub's [security advisory
form](https://github.com/PixiBixi/gopen/security/advisories/new). Please do
not open a public issue for a vulnerability.

Expect an acknowledgement within 7 days. If the report is confirmed, the fix
ships in the next release and the advisory is published once it is available.

This is a personal project maintained on a best-effort basis, with no service
level commitment.

## Scope

In scope: the code in this repository and the release artifacts published from
it (archives, checksums, attestations).

Out of scope: vulnerabilities in upstream dependencies, which belong to their
own maintainers, and issues that require an already-compromised local machine.

## Verifying a release

Releases carry a build provenance attestation tying the artifact to the
workflow and commit that produced it:

```sh
gh attestation verify <archive>.tar.gz --repo PixiBixi/gopen
```
