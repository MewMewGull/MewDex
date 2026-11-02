package localnode

import "strings"

func localID(id string) bool { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }
