package servicetray

import "embed"

//go:embed icons/*.ico icons/*.png
var roleIcons embed.FS

func iconBytes(role Role, extension string) []byte {
	if role != HubRole && role != ExecutorRole {
		role = InputRole
	}
	data, _ := roleIcons.ReadFile("icons/" + string(role) + "." + extension)
	return data
}
