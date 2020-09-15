package util

// PathIsLexicalDescendant return true if and only if
// path == maybeAncestorPath or strings.HasPrefix(path + "/", maybeAncestorPath + "/")
func PathIsLexicalDescendant(path, maybeAncestorPath string) bool {
	if len(path) < len(maybeAncestorPath) {
		return false
	}
	if path[:len(maybeAncestorPath)] != maybeAncestorPath {
		return false
	}
	if len(path) == len(maybeAncestorPath) {
		return true
	}
	return path[len(maybeAncestorPath)] == '/'
}
