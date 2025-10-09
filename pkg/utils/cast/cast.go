package cast

import (
	"fmt"
)

func ToString(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	default:
		return "", fmt.Errorf("not a string")
	}
}

func ToBytes(v any) ([]byte, error) {
	switch t := v.(type) {
	case []byte:
		return t, nil
	case string:
		return []byte(t), nil
	default:
		return nil, fmt.Errorf("not a byte string")
	}
}

func ToInt(v any) (int64, error) {
	switch t := v.(type) {
	case int:
		return int64(t), nil
	case int8:
		return int64(t), nil
	case int16:
		return int64(t), nil
	case int32:
		return int64(t), nil
	case int64:
		return t, nil
	case uint:
		return int64(t), nil
	case uint8:
		return int64(t), nil
	case uint32:
		return int64(t), nil
	case uint64:
		return int64(t), nil
	default:
		return 0, fmt.Errorf("not an int")
	}
}

func ToStringSlice(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("not a list")
	}

	out := make([]string, 0, len(list))
	for i, e := range list {
		s, err := ToString(e)
		if err != nil {
			return nil, fmt.Errorf("elem %d: %w", i, err)
		}

		out = append(out, s)
	}

	return out, nil
}

func ToTieredStrings(v any) ([][]string, error) {
	tiers, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("not list")
	}

	out := make([][]string, 0, len(tiers))
	for i, t := range tiers {
		ss, err := ToStringSlice(t)
		if err != nil || len(ss) == 0 {
			return nil, fmt.Errorf("tier %d: invalid", i)
		}

		out = append(out, ss)
	}

	return out, nil
}
