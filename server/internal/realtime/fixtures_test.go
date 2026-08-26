package realtime

import (
	"os"
)

func jsonFixture(path string) ([]byte, error) {
	return os.ReadFile(path)
}
