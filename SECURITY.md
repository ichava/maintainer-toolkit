# Security Policy

The canonical disclosure policy and STRIDE threat model live in the documentation repo:

- [Disclosure policy](https://github.com/ichava/documentation/blob/main/SECURITY.md)
- [Threat model](https://github.com/ichava/documentation/blob/main/security-threat-model.md)

## Reporting

Do not open a public GitHub issue. Use the address in the [canonical policy](https://github.com/ichava/documentation/blob/main/SECURITY.md); acknowledgement within 48 hours, patch SLA per severity.

## Scope for this package

Maintainers run this with a token that can push branches and open pull requests in every icon pack,
so the surface worth reporting is anything that widens what a sync run can reach: a source strategy
that fetches an unpinned or attacker-controlled URL, a transform that writes outside the pack root,
or a path in an upstream archive that escapes extraction.

Nothing here is installed by an application. A defect reaches users only through a pull request a
human merged.
