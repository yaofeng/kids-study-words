package dict

import "strings"

// Forms returns the base word plus the inflected forms stored in an ECDICT
// exchange string such as "p:went/d:gone/i:going/3:goes".
func Forms(word, exchange string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}

	add(word)
	for _, part := range strings.Split(exchange, "/") {
		kind, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		switch kind {
		case "p", "d", "i", "3", "r", "t", "s":
			add(value)
		}
	}
	return out
}
