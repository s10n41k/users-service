package operator

import "fmt"

func GetSQLOperator(operator string) (string, error) {
	switch operator {
	case "eq":
		return "=", nil
	case "lt":
		return "<", nil
	case "gt":
		return ">", nil
	case "lte":
		return "<=", nil
	case "gte":
		return ">=", nil
	case "between":
		return "BETWEEN", nil
	case "like":
		return "ILIKE", nil
	default:
		return "", fmt.Errorf("неизвестный оператор: %s", operator)
	}
}
