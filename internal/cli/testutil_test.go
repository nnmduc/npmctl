package cli

import "os"

// readFile is a small helper so tests can assert on raw journal contents.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
