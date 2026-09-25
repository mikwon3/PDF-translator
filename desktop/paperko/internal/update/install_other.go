//go:build !darwin && !windows

package update

func install(string) error { return ErrUnsupported }
