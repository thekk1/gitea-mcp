package main

import (
	"path"
	"runtime/debug"
	"strconv"
	"strings"

	"gitea.com/gitea/gitea-mcp/cmd"
	"gitea.com/gitea/gitea-mcp/internal/flag"
)

var Version = "dev"

func init() {
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			Version = resolveVersion(Version, info)
		}
	}
	flag.Version = Version
}

// resolveVersion returns the version reported by debug.ReadBuildInfo when its
// major version matches the major version encoded in the module path (e.g.
// "/v2" suffix). Otherwise it falls back to devVersion, since Go's module
// versioning rules make a mismatched major version untrustworthy
// (see https://gitea.com/gitea/gitea-mcp/issues/231).
func resolveVersion(devVersion string, info *debug.BuildInfo) string {
	if info == nil {
		return devVersion
	}
	buildVersion := info.Main.Version
	if buildVersion == "" || buildVersion == "(devel)" {
		return devVersion
	}

	// v0 and v1 share the suffix-less module path, so v0 pseudo-versions are valid.
	buildMajor := majorVersionOf(buildVersion)
	if buildMajor == 0 {
		buildMajor = 1
	}
	pathMajor := majorVersionFromModulePath(info.Main.Path)
	if buildMajor != pathMajor {
		return devVersion
	}

	return buildVersion
}

// majorVersionOf extracts the numeric major version from a semver-like
// string such as "v1.2.3", returning -1 if it cannot be parsed.
func majorVersionOf(version string) int {
	version = strings.TrimPrefix(version, "v")
	dot := strings.IndexByte(version, '.')
	if dot >= 0 {
		version = version[:dot]
	}
	major, err := strconv.Atoi(version)
	if err != nil {
		return -1
	}
	return major
}

// majorVersionFromModulePath returns the major version encoded in a module
// path's "/vN" suffix, or 1 if the module path has no such suffix (as is the
// case for v0 and v1 modules).
func majorVersionFromModulePath(modulePath string) int {
	suffix := path.Base(modulePath)
	if len(suffix) < 2 || suffix[0] != 'v' {
		return 1
	}
	major, err := strconv.Atoi(suffix[1:])
	if err != nil {
		return 1
	}
	return major
}

func main() {
	cmd.Execute()
}
