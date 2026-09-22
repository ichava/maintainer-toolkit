# Security Policy

## Reporting

Do not open a public GitHub issue for a security report. Two channels, in order of preference:

1. **GitHub private vulnerability reporting**, from this repository's Security tab.
2. **Email `security@simtabi.com`**, if you would rather not use GitHub.

Acknowledgement within 48 hours.

## Scope for this package

Maintainers run this with a token that can push branches and open pull requests in every icon pack,
so the surface worth reporting is anything that widens what a sync run can reach: a source strategy
that fetches an unpinned or attacker-controlled URL, a transform that writes outside the pack root,
or a path in an upstream archive that escapes extraction.

Nothing here is installed by an application. A defect reaches users only through a pull request a
human merged.
