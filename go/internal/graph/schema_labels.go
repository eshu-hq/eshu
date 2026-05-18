package graph

// labelToSnake converts a PascalCase label to lower_snake_case for use in
// constraint names (e.g., "CrossplaneXRD" -> "crossplane_x_r_d").
func labelToSnake(label string) string {
	result := make([]byte, 0, len(label)+4)
	for i, b := range []byte(label) {
		if b >= 'A' && b <= 'Z' {
			lower := b + ('a' - 'A')
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, lower)
		} else {
			result = append(result, b)
		}
	}
	return string(result)
}
