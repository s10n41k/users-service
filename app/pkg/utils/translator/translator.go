package translator

func Translator(num string) string {
	if num == "1" {
		return "not_completed"
	}
	if num == "2" {
		return "in_progress"
	}
	if num == "3" {
		return "completed"
	}
	return ""
}

func AntiTranslator(status string) string {
	if status == "not_completed" {
		return "1"
	}
	if status == "in_progress" {
		return "2"
	}
	if status == "completed" {
		return "3"
	}
	return ""
}
