// This package is part of a workaround to get the docker parser without its dependencies.
// This workaround can be removed when https://github.com/containers/image/issues/445 is done.
package shell

func equalEnvKeys(from, to string) bool {
	return from == to
}
