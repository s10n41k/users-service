package model

import "encoding/json"

func ValidateUserDTO(user UserCreateDTO) (bool, string) {
	var validationErrors []string

	if user.Email == "" {
		validationErrors = append(validationErrors, "Not entered email")
	}
	if user.Name == "" {
		validationErrors = append(validationErrors, "Not entered name")
	}
	if user.Password == "" {
		validationErrors = append(validationErrors, "Not entered password")
	}

	if len(validationErrors) > 0 {
		return false, jsonError(validationErrors)
	}

	return true, ""
}

func jsonError(errors []string) string {
	errorResponse := struct {
		Errors []string `json:"errors"`
	}{
		Errors: errors,
	}

	response, _ := json.Marshal(errorResponse)
	return string(response)
}
