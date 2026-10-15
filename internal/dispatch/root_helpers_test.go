package dispatch

// baseRef is baseRefFrom for a repo with no root branch of its own, which is
// what most of the root tests are about.
func baseRef(repoPath string, root Root) (string, error) {
	return baseRefFrom(repoPath, root, "")
}
