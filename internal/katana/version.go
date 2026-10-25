package katana

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var versionRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// Version runs katana -version -duc (3s at most) and returns the version it prints on stderr, e.g.
// "[INF] Current version: v1.1.2", or "" when it could not be read.
func Version(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, "-version", "-duc").CombinedOutput()
	return ParseVersion(string(out))
}

// ParseVersion is the first X.Y.Z in s, without the "v", or "".
func ParseVersion(s string) string {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// Untested is true when installed has another major version than tested, or is older. A newer minor or
// patch version is not: the weekly drift job tests those. An unparseable version is not either.
func Untested(installed, tested string) bool {
	a, b := split(installed), split(tested)
	if a == nil || b == nil {
		return false
	}
	if a[0] != b[0] {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func split(v string) []int {
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return nil
	}
	out := make([]int, 3)
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// VersionHint is the engine_version error hint, or "" when the installed version needs no warning.
func VersionHint(installed, tested, install string) string {
	if !Untested(installed, tested) {
		return ""
	}
	return fmt.Sprintf("katana %s is not the tested %s; results may be incomplete. Install the tested version: %s", installed, tested, install)
}
