// The module lives in src/, not at the repository root, per the Go layout in
// /opensource/CLAUDE.md. GitHub's go-import meta tag has no subdir field, so Go
// falls back to its built-in rule -- repo root is the first three path elements
// and the rest is a subdirectory. Hence the /src suffix, and hence every
// release needs a second `src/vX.Y.Z` tag alongside `vX.Y.Z`.
//
//	go install github.com/ichava/maintainer-toolkit/src/cmd/imt@latest
//
// No third-party dependencies. Everything here is stdlib, which keeps the
// release reproducible and the Docker image a static binary on a bare base.
module github.com/ichava/maintainer-toolkit/src

go 1.26
