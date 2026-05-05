// Package repositoryidentity derives canonical repository identity from the
// remote URL and local path of a checked-out repo.
//
// NormalizeRemoteURL collapses SSH and HTTPS git remotes to a single
// lower-cased https://host/path form. RepoSlugFromRemoteURL extracts the
// org/repo slug from that form. CanonicalRepositoryID returns a stable
// "repository:r_<8-hex>" identifier hashed from the remote URL when present
// and the absolute local path otherwise. A blank local path is rejected when
// no remote is available.
package repositoryidentity
