package main

import "strings"

// Fixed categories preserve a useful cause without exposing upstream URLs/tokens.
func mediaErrorCategory(raw string) string {
	value := strings.ToLower(raw)
	for _, pair := range [][2]string{{"401", "auth"}, {"403", "auth"}, {"unauthorized", "auth"}, {"auth", "auth"}, {"timeout", "timeout"}, {"deadline", "timeout"}, {"refused", "offline"}, {"not found", "not_found"}, {"codec", "unsupported_codec"}, {"unsupported", "unsupported_codec"}} {
		if strings.Contains(value, pair[0]) {
			return pair[1]
		}
	}
	return "camera_unavailable"
}
