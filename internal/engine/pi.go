package engine

// Pi returns the detector for the Pi coding agent.
//
// Pi keeps provider credentials in its own configuration and has no login
// status command we rely on, so detection stops at "installed + version" and
// reports the login state as unknown.
func Pi() Detector {
	return NewCLIDetector("pi", "pi", []string{"--version"}, nil)
}
