// Package version carries build metadata injected at link time.
//
//	go build -ldfs "-X github.com/analeis/highjack/server/internal/version.Version=1.2.3"
package version

var (
	// Version is the semantic version of the server binary. "dev" when
	// built without ldflags (e.g. `go run`).
	Version = "dev"
	// Commit is the git commit the binary was built from, if provided.
	Commit = "unknown"
)
