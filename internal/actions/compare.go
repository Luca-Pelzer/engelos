package actions

import (
	"fmt"
	"strconv"
	"strings"
)

// compareOp applies op to the (left, right) pair for cond:output and
// flow:stop-if. When BOTH sides parse as base-10 numbers the comparison is
// numeric; otherwise eq/ne compare the raw strings, gt/lt fall back to
// lexicographic order, and contains asks whether left holds right as a
// substring. An empty op means eq; an unknown op is an error.
func compareOp(op, left, right string) (bool, error) {
	lf, lok := parseNumber(left)
	rf, rok := parseNumber(right)
	numeric := lok && rok
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "", "eq":
		if numeric {
			return lf == rf, nil
		}
		return left == right, nil
	case "ne":
		if numeric {
			return lf != rf, nil
		}
		return left != right, nil
	case "gt":
		if numeric {
			return lf > rf, nil
		}
		return left > right, nil
	case "lt":
		if numeric {
			return lf < rf, nil
		}
		return left < right, nil
	case "contains":
		return strings.Contains(left, right), nil
	default:
		return false, fmt.Errorf("unknown op %q (want eq|ne|gt|lt|contains)", op)
	}
}

// parseNumber reports whether s is a base-10 number and returns its value.
func parseNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
