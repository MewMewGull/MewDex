package runtimefiles

// EngineVersion verifies the complete existing resource set without executing
// it. Migration may accept only versions in the adapter's compatibility list.
func EngineVersion(source string) (string, error) {
	set, err := openEngine(source, "")
	if err != nil {
		return "", err
	}
	defer set.close()
	return set.manifest.Version, nil
}

// EngineKind identifies an integrity-checked runtime before selecting its role.
// Older manifests describe the complete Codex runtime implicitly.
func EngineKind(source string) (string, error) {
	set, err := openEngine(source, "")
	if err != nil {
		return "", err
	}
	defer set.close()
	if set.manifest.Kind == "" {
		return "codex", nil
	}
	return set.manifest.Kind, nil
}
